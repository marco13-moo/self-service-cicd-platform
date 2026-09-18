package policy

import "testing"

func TestVerifyProducesTenantScopedShortLivedDecision(t *testing.T) {
	decision, err := Verify("acme", Declaration{
		Confidential:       true,
		AttestationProfile: "sev-snp-v1",
		AllowedResidencies: []string{"eu-west"},
		Residency:          "eu-west",
		IdentityProvider:   "aws",
		IdentityAudience:   "workload",
		EncryptionKey:      "tenant-acme",
		SecretReferences:   []string{"vault://acme/api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.PolicyDigest == "" || decision.CredentialTTL != 600 || decision.NetworkIsolation == "" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestVerifyRejectsCrossTenantKeyAndResidency(t *testing.T) {
	_, err := Verify("acme", Declaration{
		AllowedResidencies: []string{"us-east"},
		Residency:          "eu-west",
		IdentityProvider:   "aws",
		IdentityAudience:   "workload",
		EncryptionKey:      "tenant-other",
	})
	if err == nil {
		t.Fatal("expected policy rejection")
	}
}
