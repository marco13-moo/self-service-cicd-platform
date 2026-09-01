package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

type Authenticator struct {
	appID   string
	key     *rsa.PrivateKey
	client  *http.Client
	baseURL string
	mu      sync.Mutex
	tokens  map[string]scm.InstallationToken
	now     func() time.Time
}

func NewAuthenticator(appID string, privateKeyPEM []byte) (*Authenticator, error) {
	if strings.TrimSpace(appID) == "" {
		return nil, fmt.Errorf("GitHub App ID is required")
	}
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &Authenticator{appID: appID, key: key, client: &http.Client{Timeout: 10 * time.Second}, baseURL: "https://api.github.com", tokens: make(map[string]scm.InstallationToken), now: time.Now}, nil
}

func (*Authenticator) Provider() scm.Provider { return scm.ProviderGitHub }

func (a *Authenticator) Token(ctx context.Context, installationID string) (scm.InstallationToken, error) {
	if _, err := strconv.ParseInt(installationID, 10, 64); err != nil {
		return scm.InstallationToken{}, fmt.Errorf("invalid GitHub installation ID")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if token, ok := a.tokens[installationID]; ok && token.ExpiresAt.After(a.now().Add(time.Minute)) {
		return token, nil
	}
	jwt, err := a.appJWT()
	if err != nil {
		return scm.InstallationToken{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.baseURL, "/")+"/app/installations/"+installationID+"/access_tokens", nil)
	if err != nil {
		return scm.InstallationToken{}, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := a.client.Do(req)
	if err != nil {
		return scm.InstallationToken{}, fmt.Errorf("exchange GitHub installation token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return scm.InstallationToken{}, fmt.Errorf("exchange GitHub installation token: %s", resp.Status)
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || payload.Token == "" {
		return scm.InstallationToken{}, fmt.Errorf("decode GitHub installation token")
	}
	token := scm.InstallationToken{Value: payload.Token, ExpiresAt: payload.ExpiresAt}
	a.tokens[installationID] = token
	return token, nil
}

func (a *Authenticator) appJWT() (string, error) {
	now := a.now().UTC()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]interface{}{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": a.appID})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(value []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(value)
	if block == nil {
		return nil, fmt.Errorf("decode GitHub App private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("GitHub App private key is not RSA")
	}
	return key, nil
}
