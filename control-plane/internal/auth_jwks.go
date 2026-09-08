package auth

import "time"

// JWKSCache is a minimal placeholder for a JWKS fetcher/cache that supports rotation.
// TODO: implement HTTP fetching, backoff, verification, and atomic keyset swap with grace window.
type JWKSCache struct {
	URL         string
	LastUpdated time.Time
	Keys        map[string]string // kid -> public key material (opaque placeholder)
}

func NewJWKSCache(url string) *JWKSCache {
	return &JWKSCache{URL: url, LastUpdated: time.Time{}, Keys: map[string]string{}}
}

// Refresh updates the cached keyset from the remote JWKS URL. Implement exponential backoff and validation.
func (c *JWKSCache) Refresh() error {
	// TODO: fetch JWKS, validate structure, and rotate keys while keeping a grace period for old kids.
	return nil
}
