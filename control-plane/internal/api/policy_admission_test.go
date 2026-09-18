package api

import (
	"testing"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/catalog"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/policy"
)

func TestCreateServiceRequestNormalizePreservesVersionedPolicy(t *testing.T) {
	request := CreateServiceRequest{
		APIVersion: catalog.APIVersion,
		Kind:       catalog.Kind,
		Metadata:   &catalog.Metadata{Name: "payments"},
		Spec: &catalog.Spec{
			Owner:      "team-payments",
			Repository: "https://github.com/example/payments",
			Policy:     policy.Declaration{EncryptionKey: "tenant-default"},
		},
	}
	request.Normalize()
	if request.Deployment == nil || request.Deployment.Policy.EncryptionKey != "tenant-default" {
		t.Fatalf("versioned policy was not preserved: %+v", request.Deployment)
	}
}
