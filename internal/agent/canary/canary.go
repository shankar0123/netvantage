// Package canary defines the interface for all synthetic test types.
//
// Each canary type (ping, dns, http, traceroute) implements this interface
// and is compiled into the agent binary. We use Go interfaces — NOT
// plugin.Open — because Go's plugin system is fragile, version-coupled,
// and untestable. Community extensions use build tags or fork-and-compile.
package canary

import (
	"context"
	"encoding/json"
	"time"
)

// Canary is the interface that all synthetic test types must implement.
type Canary interface {
	// Type returns the canary type identifier (e.g., "ping", "dns", "http", "traceroute").
	Type() string

	// Execute runs the synthetic test as defined by the TestDefinition and
	// returns a Result. It must respect context cancellation for graceful shutdown.
	Execute(ctx context.Context, test TestDefinition) (*Result, error)

	// Validate checks whether the canary-specific configuration in config
	// is valid. Called once at config load time, not per-execution.
	Validate(config json.RawMessage) error
}

// TestDefinition describes a single synthetic test to execute.
type TestDefinition struct {
	// ID uniquely identifies this test definition (from the control plane).
	ID string `json:"test_id"`

	// Type is the canary type: "ping", "dns", "http", "traceroute".
	Type string `json:"test_type"`

	// Target is the host, URL, or IP to test against.
	Target string `json:"target"`

	// Interval is how often this test should be executed.
	Interval time.Duration `json:"interval"`

	// Timeout is the maximum time allowed for a single test execution.
	Timeout time.Duration `json:"timeout"`

	// Config holds canary-type-specific configuration (e.g., packet count
	// for ping, record type for DNS, HTTP method and headers for HTTP).
	Config json.RawMessage `json:"config"`
}

// Result is the output of a single canary execution.
type Result struct {
	// TestID references the test definition that produced this result.
	TestID string `json:"test_id"`

	// AgentID identifies the agent that executed this test.
	AgentID string `json:"agent_id"`

	// POPName is the Point of Presence that ran the test.
	POPName string `json:"pop_name"`

	// TestType is the canary type that produced this result.
	TestType string `json:"test_type"`

	// Target is the host/URL that was tested.
	Target string `json:"target"`

	// Timestamp is when the test execution started (UTC).
	Timestamp time.Time `json:"timestamp"`

	// DurationMS is the total test execution time in milliseconds.
	DurationMS float64 `json:"duration_ms"`

	// Success indicates whether the test passed all assertions.
	Success bool `json:"success"`

	// Metrics holds canary-type-specific metrics as a JSON object.
	// For ping: {rtt_min, rtt_avg, rtt_max, rtt_stddev, packet_loss, jitter}
	// For dns:  {resolution_time_ms, response_code, resolved_values}
	// For http: {dns_ms, tcp_ms, tls_ms, ttfb_ms, total_ms, status_code}
	// For traceroute: {hops: [{hop_number, ip, rtt_min, ...}]}
	Metrics json.RawMessage `json:"metrics"`

	// Error contains an error message if the test failed. Empty on success.
	Error string `json:"error,omitempty"`

	// Metadata holds optional key-value pairs (tags, version, etc.).
	Metadata map[string]string `json:"metadata,omitempty"`
}
