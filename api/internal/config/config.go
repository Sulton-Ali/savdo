// Package config loads the API's configuration from environment variables
// into a single struct at startup. Missing required values fail fast.
package config

import "github.com/caarlos0/env/v11"

// Config holds every environment-derived setting the api and savdo binaries
// need. DatabaseURL is optional here because cmd/api does not open a pool
// yet (Phase 1); cmd/savdo requires it itself when running migrations.
type Config struct {
	Addr        string `env:"API_ADDR" envDefault:":8080"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL string `env:"DATABASE_URL"`
}

// Load parses the environment into a Config, applying defaults.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
