package config

import (
	"os"
	"time"
)

func Load() *Config {
	return &Config{
		ServiceName: "self-service-cicd-control-plane",
		Environment: getEnv("ENVIRONMENT", "local"),
		HTTP: HTTPConfig{
			Address:         getEnv("HTTP_ADDRESS", ":8080"),
			ReadTimeout:     5 * time.Second,
			WriteTimeout:    10 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		Log: LogConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
		State: StateConfig{Path: getEnv("STATE_PATH", "/var/lib/control-plane/state.json")},
		Argo: ArgoConfig{
			Namespace: getEnv("ARGO_NAMESPACE", "argo"),
			UIBaseURL: getEnv("ARGO_UI_BASE_URL", "http://argo-server.argo.svc"),
		},
		GitHub:     GitHubConfig{WebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"), AppID: os.Getenv("GITHUB_APP_ID"), PrivateKeyPath: os.Getenv("GITHUB_PRIVATE_KEY_PATH")},
		Bitbucket:  BitbucketConfig{WebhookSecret: os.Getenv("BITBUCKET_WEBHOOK_SECRET"), OAuthClientID: os.Getenv("BITBUCKET_OAUTH_CLIENT_ID"), OAuthClientSecret: os.Getenv("BITBUCKET_OAUTH_CLIENT_SECRET")},
		Reconciler: ReconcilerConfig{PreviewTTL: getDurationEnv("PREVIEW_ENVIRONMENT_TTL", 2*time.Hour)},
		Database:   DatabaseConfig{URL: os.Getenv("DATABASE_URL")},
		Preview: PreviewConfig{
			ImageRepository:         os.Getenv("PREVIEW_IMAGE_REPOSITORY"),
			BaseDomain:              os.Getenv("PREVIEW_BASE_DOMAIN"),
			URLScheme:               getEnv("PREVIEW_URL_SCHEME", "https"),
			BuilderImage:            getEnv("PREVIEW_BUILDER_IMAGE", "moby/buildkit:v0.33.0-rootless"),
			RegistrySecretName:      getEnv("PREVIEW_REGISTRY_SECRET", "registry-credentials"),
			RegistryInsecure:        getEnvBool("PREVIEW_REGISTRY_INSECURE", false),
			ScannerImage:            getEnv("PREVIEW_SCANNER_IMAGE", "aquasec/trivy:0.74.0"),
			VulnerabilitySeverities: getEnv("PREVIEW_VULNERABILITY_SEVERITIES", "CRITICAL"),
			IgnoreUnfixed:           getEnvBool("PREVIEW_VULNERABILITY_IGNORE_UNFIXED", true),
			TargetPlatform:          getEnv("PREVIEW_TARGET_PLATFORM", "linux/amd64"),
			CosignImage:             getEnv("PREVIEW_COSIGN_IMAGE", "ghcr.io/sigstore/cosign/cosign:v2.6.4"),
			CosignSigner:            getEnv("PREVIEW_COSIGN_SIGNER", "/cosign-private/cosign.key"),
			SigningProfile:          getEnv("PREVIEW_SIGNING_PROFILE", "key"),
			CosignAuthMode:          getEnv("PREVIEW_COSIGN_AUTH_MODE", "ambient"),
			VaultImage:              getEnv("PREVIEW_VAULT_IMAGE", "hashicorp/vault:1.20.4"),
			VaultAddress:            os.Getenv("PREVIEW_VAULT_ADDR"),
			VaultRole:               getEnv("PREVIEW_VAULT_ROLE", "self-service-cicd-signer"),
			CosignPrivateKeySecret:  getEnv("PREVIEW_COSIGN_PRIVATE_KEY_SECRET", "preview-cosign-private"),
			CosignPublicKeySecret:   getEnv("PREVIEW_COSIGN_PUBLIC_KEY_SECRET", "preview-cosign-public"),
			PolicyPredicateType:     getEnv("PREVIEW_POLICY_PREDICATE_TYPE", "https://self-service-cicd.dev/attestations/vulnerability-policy/v1"),
			VEXConfigMap:            getEnv("PREVIEW_VEX_CONFIGMAP", "preview-vex-none"),
		},
	}
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value == "true" || value == "1"
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
