package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maximumJWKSBytes = 1 << 20

type verificationKey struct {
	Algorithm string
	Key       any
}

type keySet map[string]verificationKey

// JWKSCache retains one bounded predecessor set during provider rollover. It
// serializes refreshes so an unknown kid cannot induce an outbound request
// stampede across concurrent authentication attempts.
type JWKSCache struct {
	url             string
	client          *http.Client
	refreshInterval time.Duration
	gracePeriod     time.Duration
	staleAfter      time.Duration

	mu              sync.RWMutex
	refreshMu       sync.Mutex
	current         keySet
	previous        keySet
	previousExpires time.Time
	refreshedAt     time.Time
	nextRefresh     time.Time
	fingerprint     [sha256.Size]byte
}

func NewJWKSCache(url string, client *http.Client, refreshInterval, gracePeriod, staleAfter time.Duration) *JWKSCache {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	clientCopy := *client
	priorRedirectPolicy := client.CheckRedirect
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" || request.URL.User != nil {
			return errors.New("JWKS redirect must remain credential-free HTTPS")
		}
		if priorRedirectPolicy != nil {
			return priorRedirectPolicy(request, via)
		}
		if len(via) >= 5 {
			return errors.New("too many JWKS redirects")
		}
		return nil
	}
	return &JWKSCache{url: url, client: &clientCopy, refreshInterval: refreshInterval, gracePeriod: gracePeriod, staleAfter: staleAfter}
}

func (c *JWKSCache) URL() string { return c.url }

func (c *JWKSCache) Refresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("fetch JWKS: unexpected HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumJWKSBytes+1))
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}
	if len(body) > maximumJWKSBytes {
		return errors.New("JWKS exceeds one MiB")
	}
	parsed, err := parseJWKS(body)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	interval := c.refreshInterval
	if maxAge := cacheMaxAge(response.Header.Get("Cache-Control")); maxAge > 0 && (interval <= 0 || maxAge < interval) {
		interval = maxAge
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	c.mu.Lock()
	fingerprint := sha256.Sum256(body)
	if c.current != nil && fingerprint != c.fingerprint {
		c.previous = c.current
		c.previousExpires = now.Add(c.gracePeriod)
	}
	c.current = parsed
	c.fingerprint = fingerprint
	c.refreshedAt = now
	c.nextRefresh = now.Add(interval)
	c.mu.Unlock()
	return nil
}

func (c *JWKSCache) Lookup(kid, algorithm string, now time.Time) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.staleAfter > 0 && (c.refreshedAt.IsZero() || now.After(c.refreshedAt.Add(c.staleAfter))) {
		return nil, false
	}
	if key, ok := c.current[kid]; ok && key.Algorithm == algorithm {
		return key.Key, true
	}
	if now.Before(c.previousExpires) {
		if key, ok := c.previous[kid]; ok && key.Algorithm == algorithm {
			return key.Key, true
		}
	}
	return nil, false
}

func (c *JWKSCache) RefreshDue(now time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current == nil || !now.Before(c.nextRefresh)
}

func (c *JWKSCache) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if c.RefreshDue(time.Now().UTC()) {
			if err := c.Refresh(ctx); err != nil {
				backoff *= 2
				if backoff > time.Minute {
					backoff = time.Minute
				}
			} else {
				backoff = time.Second
			}
		}
		wait := backoff
		c.mu.RLock()
		if !c.nextRefresh.IsZero() && time.Now().Before(c.nextRefresh) {
			wait = time.Until(c.nextRefresh)
		}
		c.mu.RUnlock()
		if wait < time.Second {
			wait = time.Second
		}
		// Deterministic bounded jitter prevents synchronized replicas without
		// introducing a mutable global random source.
		wait += time.Duration(time.Now().UnixNano() % int64(wait/10+1))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type jwksDocument struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func parseJWKS(data []byte) (keySet, error) {
	var document jwksDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}
	if len(document.Keys) == 0 {
		return nil, errors.New("JWKS contains no keys")
	}
	keys := make(keySet, len(document.Keys))
	for _, encoded := range document.Keys {
		if encoded.Kid == "" || encoded.Alg == "" || (encoded.Use != "" && encoded.Use != "sig") {
			return nil, errors.New("every JWKS key requires kid, alg, and signature use")
		}
		if _, duplicate := keys[encoded.Kid]; duplicate {
			return nil, fmt.Errorf("duplicate JWKS kid %q", encoded.Kid)
		}
		key, err := encoded.publicKey()
		if err != nil {
			return nil, fmt.Errorf("decode JWKS key %q: %w", encoded.Kid, err)
		}
		keys[encoded.Kid] = verificationKey{Algorithm: encoded.Alg, Key: key}
	}
	return keys, nil
}

func (k jsonWebKey) publicKey() (any, error) {
	switch k.Kty {
	case "RSA":
		allowed := map[string]bool{"RS256": true, "RS384": true, "RS512": true}
		if !allowed[k.Alg] {
			return nil, errors.New("RSA key algorithm must be RS256, RS384, or RS512")
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
			return nil, errors.New("invalid RSA exponent")
		}
		exponent := 0
		for _, value := range eBytes {
			exponent = exponent<<8 + int(value)
		}
		if exponent < 3 || new(big.Int).SetBytes(n).BitLen() < 2048 {
			return nil, errors.New("RSA key is below the 2048-bit minimum")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, nil
	case "EC":
		curves := map[string]struct {
			curve elliptic.Curve
			alg   string
		}{"P-256": {elliptic.P256(), "ES256"}, "P-384": {elliptic.P384(), "ES384"}, "P-521": {elliptic.P521(), "ES512"}}
		definition, ok := curves[k.Crv]
		if !ok || definition.alg != k.Alg {
			return nil, errors.New("unsupported EC curve or algorithm")
		}
		x, errX := base64.RawURLEncoding.DecodeString(k.X)
		y, errY := base64.RawURLEncoding.DecodeString(k.Y)
		if errX != nil || errY != nil {
			return nil, errors.New("invalid EC coordinates")
		}
		public := &ecdsa.PublicKey{Curve: definition.curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !public.Curve.IsOnCurve(public.X, public.Y) {
			return nil, errors.New("EC point is not on the declared curve")
		}
		return public, nil
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func cacheMaxAge(value string) time.Duration {
	for _, directive := range strings.Split(value, ",") {
		name, raw, found := strings.Cut(strings.TrimSpace(directive), "=")
		if found && strings.EqualFold(name, "max-age") {
			seconds, err := strconv.Atoi(strings.Trim(raw, `"`))
			if err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 0
}
