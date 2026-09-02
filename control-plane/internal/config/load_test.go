package config

import "testing"

func TestLoadPreviewBuildConfiguration(t *testing.T) {
	t.Setenv("PREVIEW_IMAGE_REPOSITORY", "registry.test/previews")
	t.Setenv("PREVIEW_BASE_DOMAIN", "preview.test")
	t.Setenv("PREVIEW_REGISTRY_INSECURE", "true")

	cfg := Load()
	if cfg.Preview.ImageRepository != "registry.test/previews" || cfg.Preview.BaseDomain != "preview.test" {
		t.Fatalf("preview routing configuration not loaded: %#v", cfg.Preview)
	}
	if !cfg.Preview.RegistryInsecure || cfg.Preview.URLScheme != "https" || cfg.Preview.BuilderImage == "" || cfg.Preview.ScannerImage != "aquasec/trivy:0.74.0" || cfg.Preview.VulnerabilitySeverities != "CRITICAL" || !cfg.Preview.IgnoreUnfixed || cfg.Preview.TargetPlatform != "linux/amd64" {
		t.Fatalf("preview build defaults not loaded: %#v", cfg.Preview)
	}
}
