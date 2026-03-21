// Package config provides configuration for the NetVantage control plane server.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// TLS configuration for the control plane API server.
	TLS TLSConfig

	// Secrets provider: "env" (default), "vault", "k8s", "sops".
	SecretsProvider string

	// Vault configuration (when SecretsProvider = "vault").
	Vault VaultConfig

	// Transport backend: "nats" (default) or "kafka".
	TransportBackend string

	// Kafka configuration (when TransportBackend = "kafka").
	Kafka KafkaConfig

	// OIDC / OAuth2 configuration for API authentication.
	OIDC OIDCConfig

	// Audit log configuration.
	AuditEnabled bool
}

// TLSConfig holds TLS settings for the server.
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	CAFile   string // For mTLS client verification.
}

// VaultConfig holds HashiCorp Vault settings.
type VaultConfig struct {
	Addr      string
	Token     string
	MountPath string // Default: "secret/data/netvantage"
	Namespace string
}

// KafkaConfig holds Kafka transport settings (loaded from env).
type KafkaConfig struct {
	Brokers       []string
	SASLEnabled   bool
	SASLMechanism string
	SASLUsername  string
	SASLPassword  string
	TLSEnabled    bool
	TLSCert       string
	TLSKey        string
	TLSCACert     string
}

// OIDCConfig holds OpenID Connect settings for API auth.
type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Audience     string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Addr:            envOrDefault("NETVANTAGE_SERVER_ADDR", ":8080"),
		DatabaseURL:     envOrDefault("NETVANTAGE_DB_URL", "postgres://netvantage:netvantage-dev@localhost:5432/netvantage?sslmode=disable"),
		JWTSecret:       envOrDefault("NETVANTAGE_JWT_SECRET", "dev-secret-change-in-production"),
		AgentStaleAfter: 5 * time.Minute,

		TLS: TLSConfig{
			Enabled:  envBool("NETVANTAGE_TLS_ENABLED"),
			CertFile: envOrDefault("NETVANTAGE_TLS_CERT", ""),
			KeyFile:  envOrDefault("NETVANTAGE_TLS_KEY", ""),
			CAFile:   envOrDefault("NETVANTAGE_TLS_CA", ""),
		},

		SecretsProvider: envOrDefault("NETVANTAGE_SECRETS_PROVIDER", "env"),

		Vault: VaultConfig{
			Addr:      envOrDefault("VAULT_ADDR", "http://127.0.0.1:8200"),
			Token:     envOrDefault("VAULT_TOKEN", ""),
			MountPath: envOrDefault("NETVANTAGE_VAULT_MOUNT", "secret/data/netvantage"),
			Namespace: envOrDefault("VAULT_NAMESPACE", ""),
		},

		TransportBackend: envOrDefault("NETVANTAGE_TRANSPORT", "nats"),

		Kafka: KafkaConfig{
			Brokers:       splitCSV(envOrDefault("NETVANTAGE_KAFKA_BROKERS", "")),
			SASLEnabled:   envBool("NETVANTAGE_KAFKA_SASL_ENABLED"),
			SASLMechanism: envOrDefault("NETVANTAGE_KAFKA_SASL_MECHANISM", "SCRAM-SHA-256"),
			SASLUsername:  envOrDefault("NETVANTAGE_KAFKA_SASL_USERNAME", ""),
			SASLPassword:  envOrDefault("NETVANTAGE_KAFKA_SASL_PASSWORD", ""),
			TLSEnabled:    envBool("NETVANTAGE_KAFKA_TLS_ENABLED"),
			TLSCert:       envOrDefault("NETVANTAGE_KAFKA_TLS_CERT", ""),
			TLSKey:        envOrDefault("NETVANTAGE_KAFKA_TLS_KEY", ""),
			TLSCACert:     envOrDefault("NETVANTAGE_KAFKA_TLS_CA", ""),
		},

		OIDC: OIDCConfig{
			Enabled:      envBool("NETVANTAGE_OIDC_ENABLED"),
			IssuerURL:    envOrDefault("NETVANTAGE_OIDC_ISSUER", ""),
			ClientID:     envOrDefault("NETVANTAGE_OIDC_CLIENT_ID", ""),
			ClientSecret: envOrDefault("NETVANTAGE_OIDC_CLIENT_SECRET", ""),
			Audience:     envOrDefault("NETVANTAGE_OIDC_AUDIENCE", ""),
		},

		AuditEnabled: envBoolDefault("NETVANTAGE_AUDIT_ENABLED", true),
	}
}

// Validate checks that required configuration is present and consistent.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("NETVANTAGE_DB_URL is required")
	}
	if c.TLS.Enabled {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return fmt.Errorf("NETVANTAGE_TLS_CERT and NETVANTAGE_TLS_KEY are required when TLS is enabled")
		}
	}
	if c.TransportBackend == "kafka" && len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("NETVANTAGE_KAFKA_BROKERS is required when transport backend is kafka")
	}
	if c.OIDC.Enabled {
		if c.OIDC.IssuerURL == "" || c.OIDC.ClientID == "" {
			return fmt.Errorf("NETVANTAGE_OIDC_ISSUER and NETVANTAGE_OIDC_CLIENT_ID are required when OIDC is enabled")
		}
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	v := os.Getenv(key)
	b, _ := strconv.ParseBool(v)
	return b
}

func envBoolDefault(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
