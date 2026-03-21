package dns

// Config holds DNS canary-specific configuration.
//
// These fields are deserialized from the "config" JSON blob in a TestDefinition.
// A minimal config only needs a target (the domain to query). Defaults query
// for A records using the system resolver.
type Config struct {
	// RecordType is the DNS record type to query.
	// Supported: A, AAAA, CNAME, MX, NS, TXT, SOA, SRV.
	// Default: "A".
	RecordType string `json:"record_type"`

	// Resolvers is a list of DNS resolver addresses to query.
	// Each resolver is an IP or IP:port. If empty, uses the system resolver.
	// Multiple resolvers enable cross-resolver comparison (e.g., compare
	// Google DNS vs Cloudflare vs your internal resolver).
	// Example: ["8.8.8.8", "1.1.1.1", "192.168.1.1:53"]
	Resolvers []string `json:"resolvers"`

	// ExpectedValues is a list of expected resolved values for content
	// validation. If set, the test fails unless ALL expected values appear
	// in the resolved result set. This catches DNS poisoning, stale records,
	// and propagation delays.
	// Example for A record: ["93.184.216.34"]
	// Example for MX record: ["10 mail.example.com."]
	ExpectedValues []string `json:"expected_values"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		RecordType: "A",
	}
}

// ResolverResult holds the result from querying a single DNS resolver.
type ResolverResult struct {
	// Resolver is the address of the DNS resolver used ("system" for default).
	Resolver string `json:"resolver"`

	// ResolutionTimeMS is the query time in milliseconds.
	ResolutionTimeMS float64 `json:"resolution_time_ms"`

	// ResponseCode is the DNS response code (NOERROR, NXDOMAIN, SERVFAIL, etc.).
	ResponseCode string `json:"response_code"`

	// ResolvedValues holds the resolved records.
	ResolvedValues []string `json:"resolved_values"`

	// Success indicates whether this resolver query succeeded and passed
	// content validation (if configured).
	Success bool `json:"success"`

	// Error holds the error message if the query failed.
	Error string `json:"error,omitempty"`
}

// Metrics holds the aggregated results of a DNS canary execution.
type Metrics struct {
	// RecordType that was queried.
	RecordType string `json:"record_type"`

	// AvgResolutionTimeMS is the average resolution time across all resolvers.
	AvgResolutionTimeMS float64 `json:"avg_resolution_time_ms"`

	// Resolvers holds per-resolver results for cross-resolver comparison.
	Resolvers []ResolverResult `json:"resolvers"`
}
