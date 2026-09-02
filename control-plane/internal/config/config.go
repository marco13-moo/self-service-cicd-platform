package config

import (
	"time"
)

type Config struct {
	ServiceName string
	Environment string

	HTTP       HTTPConfig
	Log        LogConfig
	State      StateConfig
	Argo       ArgoConfig
	GitHub     GitHubConfig
	Bitbucket  BitbucketConfig
	Reconciler ReconcilerConfig
	Database   DatabaseConfig
}

type HTTPConfig struct {
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type LogConfig struct {
	Level string
}

type StateConfig struct{ Path string }

type ArgoConfig struct {
	Namespace string
	UIBaseURL string
}

type GitHubConfig struct {
	WebhookSecret  string
	AppID          string
	PrivateKeyPath string
}

type BitbucketConfig struct{ WebhookSecret, OAuthClientID, OAuthClientSecret string }

type ReconcilerConfig struct{ PreviewTTL time.Duration }
type DatabaseConfig struct{ URL string }
