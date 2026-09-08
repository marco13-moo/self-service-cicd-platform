package auth

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"go.uber.org/zap"
)

// JWKSCache holds the current and previous key sets and supports refresh and
// a grace window during key rotation.
type JWKSCache struct {
	URL          string
	mu           sync.RWMutex
	current      jwk.Set
	previous     jwk.Set
	rotationTime time.Time
	logger       *zap.Logger
}

func NewJWKSCache(url string, logger *zap.Logger) *JWKSCache {
	return &JWKSCache{URL: url, mu: sync.RWMutex{}, logger: logger}
}

// Refresh updates the cached keyset from the remote JWKS URL. It keeps the
// previous keyset for a grace window so tokens signed with old keys remain
// valid for a short period during rollover.
func (c *JWKSCache) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Use the default HTTP client but allow observability via logger on failures.
	set, err := jwk.Fetch(ctx, c.URL, jwk.WithHTTPClient(http.DefaultClient))
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("jwks fetch failed", zap.Error(err))
		}
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// rotate
	c.previous = c.current
	c.current = set
	c.rotationTime = time.Now().UTC()
	if c.logger != nil {
		c.logger.Info("jwks refreshed", zap.String("url", c.URL), zap.Time("at", c.rotationTime))
	}
	return nil
}

// KeySet returns the current key set and the previous set (may be nil).
func (c *JWKSCache) KeySet() (jwk.Set, jwk.Set) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current, c.previous
}
