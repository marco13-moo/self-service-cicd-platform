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
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
