// Package config handles agent configuration loading, validation, and caching.
//
// Config sources (in priority order):
//   1. Environment variables (NETVANTAGE_ prefix)
//   2. YAML config file (--config flag)
//   3. Cached config (~/.netvantage/config-cache.yaml) — fallback when
//      control plane is unreachable
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"encoding/json"
)

// Config is the top-level agent configuration.
type Config struct {
	// ControlPlaneURL is the URL of the control plane API for registration
	// and config sync. Empty means standalone mode (static config only).
	ControlPlaneURL string `json:"control_plane_url" yaml:"control_plane_url"`

	// AgentID uniquely identifies this agent. Auto-generated on first run
	// and persisted to the cache directory.
	AgentID string `json:"agent_id" yaml:"agent_id"`

	// POP configuration
	POPName     string `json:"pop_name" yaml:"pop_name"`
	POPProvider string `json:"pop_provider" yaml:"pop_provider"`

	// Transport configuration
	Transport TransportConfig `json:"transport" yaml:"transport"`

	// Intervals
	HeartbeatInterval  time.Duration `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	ConfigSyncInterval time.Duration `json:"config_sync_interval" yaml:"config_sync_interval"`

	// Buffer configuration for local result caching
	Buffer BufferConfig `json:"buffer" yaml:"buffer"`

	// LogLevel controls logging verbosity: debug, info, warn, error
	LogLevel string `json:"log_level" yaml:"log_level"`

	// CacheDir is the directory for config cache and agent state.
	// Defaults to ~/.netvantage/
	CacheDir string `json:"cache_dir" yaml:"cache_dir"`

	// StaticTests are test definitions loaded from config file (used when
	// no control plane is configured or as fallback).
	StaticTests []StaticTest `json:"tests" yaml:"tests"`
}

// TransportConfig selects and configures the message transport backend.
type TransportConfig struct {
	// Backend is the transport implementation: "nats" (default) or "kafka".
	Backend string `json:"backend" yaml:"backend"`

	// NATS configuration
	NATS NATSConfig `json:"nats" yaml:"nats"`

	// Kafka configuration (M9+)
	Kafka KafkaConfig `json:"kafka" yaml:"kafka"`
}

// NATSConfig holds NATS JetStream connection settings.
type NATSConfig struct {
	URL       string `json:"url" yaml:"url"`
	TLSCert   string `json:"tls_cert" yaml:"tls_cert"`
	TLSKey    string `json:"tls_key" yaml:"tls_key"`
	TLSCACert string `json:"tls_ca_cert" yaml:"tls_ca_cert"`
}

// KafkaConfig holds Kafka connection settings (M9+ production backend).
type KafkaConfig struct {
	Brokers   []string `json:"brokers" yaml:"brokers"`
	TLSCert   string   `json:"tls_cert" yaml:"tls_cert"`
	TLSKey    string   `json:"tls_key" yaml:"tls_key"`
	TLSCACert string   `json:"tls_ca_cert" yaml:"tls_ca_cert"`
	SASLUser  string   `json:"sasl_user" yaml:"sasl_user"`
	SASLPass  string   `json:"sasl_pass" yaml:"sasl_pass"`
}

// BufferConfig controls the local disk-backed result buffer.
type BufferConfig struct {
	// Enabled controls whether local buffering is active. Default: true.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaxSizeBytes is the maximum buffer size on disk. Default: 100MB.
	MaxSizeBytes int64 `json:"max_size_bytes" yaml:"max_size_bytes"`

	// Path is the file path for the buffer database.
	// Default: <cache_dir>/buffer.db
	Path string `json:"path" yaml:"path"`
}

// StaticTest is a test definition loaded from the config file.
type StaticTest struct {
	ID       string          `json:"test_id" yaml:"test_id"`
	Type     string          `json:"test_type" yaml:"test_type"`
	Target   string          `json:"target" yaml:"target"`
	Interval time.Duration   `json:"interval" yaml:"interval"`
	Timeout  time.Duration   `json:"timeout" yaml:"timeout"`
	Config   json.RawMessage `json:"config" yaml:"config"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".netvantage")

	return &Config{
		POPName:            "default",
		HeartbeatInterval:  30 * time.Second,
		ConfigSyncInterval: 60 * time.Second,
		LogLevel:           "info",
		CacheDir:           cacheDir,
		Transport: TransportConfig{
			Backend: "nats",
			NATS: NATSConfig{
				URL: "nats://localhost:4222",
			},
		},
		Buffer: BufferConfig{
			Enabled:      true,
			MaxSizeBytes: 100 * 1024 * 1024, // 100MB
		},
	}
}

// CachePath returns the full path to the config cache file.
func (c *Config) CachePath() string {
	return filepath.Join(c.CacheDir, "config-cache.yaml")
}

// Validate checks the config for required fields and consistency.
func (c *Config) Validate() error {
	if c.POPName == "" {
		return fmt.Errorf("config: pop_name is required")
	}
	if c.Transport.Backend != "nats" && c.Transport.Backend != "kafka" {
		return fmt.Errorf("config: transport.backend must be 'nats' or 'kafka', got %q", c.Transport.Backend)
	}
	if c.Transport.Backend == "nats" && c.Transport.NATS.URL == "" {
		return fmt.Errorf("config: transport.nats.url is required when backend is 'nats'")
	}
	if c.Transport.Backend == "kafka" && len(c.Transport.Kafka.Brokers) == 0 {
		return fmt.Errorf("config: transport.kafka.brokers is required when backend is 'kafka'")
	}
	return nil
}
