package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var tenantKeyName = regexp.MustCompile(`^tenant-[a-z0-9][a-z0-9-]{0,61}[a-z0-9]$`)

type Declaration struct {
	Confidential         bool     `json:"confidential,omitempty"`
	AttestationProfile   string   `json:"attestation_profile,omitempty"`
	AllowedResidencies   []string `json:"allowed_residencies,omitempty"`
	Residency            string   `json:"residency,omitempty"`
	IdentityProvider     string   `json:"identity_provider,omitempty"`
	IdentityAudience     string   `json:"identity_audience,omitempty"`
	CredentialTTLSeconds int      `json:"credential_ttl_seconds,omitempty"`
	SecretReferences     []string `json:"secret_references,omitempty"`
	EncryptionKey        string   `json:"encryption_key,omitempty"`
}

type AdmissionDecision struct {
	PolicyDigest       string   `json:"policy_digest"`
	Confidential       bool     `json:"confidential"`
	AttestationProfile string   `json:"attestation_profile,omitempty"`
	Residency          string   `json:"residency"`
	IdentityProvider   string   `json:"identity_provider"`
	IdentityAudience   string   `json:"identity_audience"`
	CredentialTTL      int      `json:"credential_ttl_seconds"`
	SecretReferences   []string `json:"secret_references,omitempty"`
	EncryptionKey      string   `json:"encryption_key"`
	NetworkIsolation   string   `json:"network_isolation"`
}

func Verify(tenantID string, declaration Declaration) (AdmissionDecision, error) {
	tenantID = strings.TrimSpace(strings.ToLower(tenantID))
	if tenantID == "" {
		return AdmissionDecision{}, errors.New("tenant identity is required")
	}
	if len(declaration.AllowedResidencies) == 0 && declaration.Residency == "" &&
		declaration.IdentityProvider == "" && declaration.IdentityAudience == "" &&
		declaration.EncryptionKey == "" && !declaration.Confidential {
		declaration.AllowedResidencies = []string{"unspecified"}
		declaration.Residency = "unspecified"
		declaration.IdentityProvider = "platform"
		declaration.IdentityAudience = "tenant-workload"
	}
	if declaration.Confidential && strings.TrimSpace(declaration.AttestationProfile) == "" {
		return AdmissionDecision{}, errors.New("confidential workloads require an attestation profile")
	}
	attestationProfile := strings.TrimSpace(declaration.AttestationProfile)
	if attestationProfile == "" {
		attestationProfile = "none"
	}
	if len(declaration.AllowedResidencies) == 0 {
		return AdmissionDecision{}, errors.New("at least one allowed data residency is required")
	}
	residency := strings.TrimSpace(strings.ToLower(declaration.Residency))
	if residency == "" {
		return AdmissionDecision{}, errors.New("data residency is required")
	}
	allowed := false
	for _, candidate := range declaration.AllowedResidencies {
		if residency == strings.TrimSpace(strings.ToLower(candidate)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return AdmissionDecision{}, fmt.Errorf("residency %q is not allowed by tenant policy", residency)
	}
	provider := strings.TrimSpace(declaration.IdentityProvider)
	audience := strings.TrimSpace(declaration.IdentityAudience)
	if provider == "" || audience == "" {
		return AdmissionDecision{}, errors.New("workload identity provider and audience are required")
	}
	ttl := declaration.CredentialTTLSeconds
	if ttl == 0 {
		ttl = 600
	}
	if ttl < 60 || ttl > int((15*time.Minute).Seconds()) {
		return AdmissionDecision{}, errors.New("short-lived credential TTL must be between 60 and 900 seconds")
	}
	key := strings.TrimSpace(strings.ToLower(declaration.EncryptionKey))
	if key == "" {
		key = "tenant-" + tenantID
	}
	if !tenantKeyName.MatchString(key) || !strings.HasPrefix(key, "tenant-"+tenantID) {
		return AdmissionDecision{}, errors.New("encryption key must be tenant-scoped")
	}
	refs := append([]string(nil), declaration.SecretReferences...)
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" || strings.ContainsAny(ref, " \t\r\n=") {
			return AdmissionDecision{}, errors.New("secret references must be non-empty external references")
		}
	}
	digestInput, _ := json.Marshal(declaration)
	digest := sha256.Sum256(digestInput)
	return AdmissionDecision{
		PolicyDigest:       "sha256:" + hex.EncodeToString(digest[:]),
		Confidential:       declaration.Confidential,
		AttestationProfile: attestationProfile,
		Residency:          residency,
		IdentityProvider:   provider,
		IdentityAudience:   audience,
		CredentialTTL:      ttl,
		SecretReferences:   refs,
		EncryptionKey:      key,
		NetworkIsolation:   "default-deny-tenant-egress",
	}, nil
}
