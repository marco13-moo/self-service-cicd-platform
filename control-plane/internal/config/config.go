package config

import (
	"time"
)

type Config struct {
	ServiceName string
	Environment string

	HTTP  HTTPConfig
	Log   LogConfig
	State StateConfig
	Argo  ArgoConfig
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
