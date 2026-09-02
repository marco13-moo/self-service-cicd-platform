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
	}
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
