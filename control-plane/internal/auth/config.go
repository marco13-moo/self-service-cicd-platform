// Package auth implements the cryptographic half of tenant authentication.
// Authorization and persistence deliberately remain in the API package.
package auth

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

type ProviderConfig struct {
	Issuer       string            `json:"issuer"`
	JWKSURL      string            `json:"jwks_url"`
	Audiences    []string          `json:"audiences"`
	TenantID     string            `json:"tenant_id"`
	TenantClaim  string            `json:"tenant_claim,omitempty"`
	SubjectClaim string            `json:"subject_claim,omitempty"`
	GroupsClaim  string            `json:"groups_claim,omitempty"`
	GroupRoles   map[string]string `json:"group_roles"`
}

func (c ProviderConfig) Validate(allowLoopbackHTTP bool) error {
	if strings.TrimSpace(c.Issuer) == "" || strings.TrimSpace(c.JWKSURL) == "" || strings.TrimSpace(c.TenantID) == "" || len(c.Audiences) == 0 {
		return errors.New("issuer, jwks_url, tenant_id, and at least one audience are required")
	}
	issuer, err := url.Parse(c.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("issuer must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	jwksURL, err := url.Parse(c.JWKSURL)
	if err != nil || jwksURL.Host == "" || jwksURL.User != nil || jwksURL.Fragment != "" {
		return errors.New("jwks_url must be an absolute URL without credentials or fragment")
	}
	if jwksURL.Scheme != "https" {
		host := jwksURL.Hostname()
		if !allowLoopbackHTTP || jwksURL.Scheme != "http" || (host != "localhost" && net.ParseIP(host) == nil) || (net.ParseIP(host) != nil && !net.ParseIP(host).IsLoopback()) {
			return errors.New("jwks_url must use HTTPS; HTTP is permitted only for loopback tests")
		}
	}
	for _, audience := range c.Audiences {
		if strings.TrimSpace(audience) == "" {
			return errors.New("audiences cannot contain an empty value")
		}
	}
	for _, role := range c.GroupRoles {
		if role != "viewer" && role != "developer" && role != "admin" {
			return errors.New("group role must be viewer, developer, or admin")
		}
	}
	return nil
}
