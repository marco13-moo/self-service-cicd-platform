// Command oidc-conformance validates a token issued by an external OIDC
// provider through exactly the same cryptographic boundary as the API server.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	platformauth "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/auth"
)

func main() {
	issuer := flag.String("issuer", "", "expected token issuer")
	jwksURL := flag.String("jwks-url", "", "provider JWKS URL")
	audience := flag.String("audience", "control-plane", "required audience")
	tenant := flag.String("tenant", "oidc-conformance", "immutable tenant capability")
	token := flag.String("token", "", "JWT access token")
	expectRole := flag.String("expect-role", "developer", "expected mapped role")
	expectFailure := flag.Bool("expect-failure", false, "require validation to fail")
	insecureLoopbackTLS := flag.Bool("insecure-loopback-tls", false, "trust a self-signed loopback certificate for local conformance only")
	flag.Parse()

	provider := platformauth.ProviderConfig{
		Issuer: *issuer, JWKSURL: *jwksURL, Audiences: []string{*audience},
		TenantID: *tenant, GroupsClaim: "groups",
		GroupRoles: map[string]string{"developers": "developer", "administrators": "admin"},
	}
	if err := provider.Validate(true); err != nil {
		fatalf("invalid provider configuration: %v", err)
	}
	var client *http.Client
	if *insecureLoopbackTLS {
		parsed, err := url.Parse(*jwksURL)
		if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1") {
			fatalf("insecure TLS is restricted to an HTTPS loopback JWKS endpoint")
		}
		// This is deliberately unreachable from the production server. It
		// permits a disposable provider to exercise the HTTPS-only contract.
		client = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec
	}
	cache := platformauth.NewJWKSCache(*jwksURL, client, time.Minute, fiveMinutes, fifteenMinutes)
	if err := cache.Refresh(context.Background()); err != nil {
		fatalf("refresh JWKS: %v", err)
	}
	identity, err := platformauth.NewValidator(cache).Validate(context.Background(), *token, provider)
	if *expectFailure {
		if err == nil {
			fatalf("token unexpectedly validated as %s", identity.Subject)
		}
		fmt.Println("OIDC token failed closed as expected")
		return
	}
	if err != nil {
		fatalf("validate token: %v", err)
	}
	if identity.TenantID != *tenant || identity.Role != *expectRole {
		fatalf("unexpected capability tenant=%s role=%s", identity.TenantID, identity.Role)
	}
	fmt.Printf("OIDC token validated subject=%s tenant=%s role=%s\n", identity.Subject, identity.TenantID, identity.Role)
}

const (
	fiveMinutes    = 5 * time.Minute
	fifteenMinutes = 15 * time.Minute
)

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
