// Package config provides configuration for the NetVantage control plane server.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all server configuration.
type Config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   string

	// Agent staleness threshold — agents without a heartbeat for this
	// duration are marked offline.
	AgentStaleAfter time.Duration
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Addr:            envOrDefault("NETVANTAGE_SERVER_ADDR", ":8080"),
		DatabaseURL:     envOrDefault("NETVANTAGE_DB_URL", "postgres://netvantage:netvantage-dev@localhost:5432/netvantage?sslmode=disable"),
		JWTSecret:       envOrDefault("NETVANTAGE_JWT_SECRET", "dev-secret-change-in-production"),
		AgentStaleAfter: 5 * time.Minute,
	}
}

// Validate checks that required configuration is present.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("NETVANTAGE_DB_URL is required")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
