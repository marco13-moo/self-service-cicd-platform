package config

import (
	"time"
)

type Config struct {
	ServiceName string
	Environment string

	HTTP HTTPConfig
	Log  LogConfig
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
