package ping

import "time"

// Config holds ping canary-specific configuration.
//
// These fields are deserialized from the "config" JSON blob in a TestDefinition.
// All fields have sensible defaults — a minimal config only needs a target.
type Config struct {
	// Count is the number of ICMP echo requests to send per execution.
	// Default: 5. Higher counts give more statistically meaningful results
	// but take longer.
	Count int `json:"count"`

	// Interval is the time between sending consecutive echo requests.
	// Default: 200ms. Lower values give faster results but may trigger
	// rate limiting on some hosts.
	Interval time.Duration `json:"interval"`

	// PayloadSize is the size of the ICMP payload in bytes (not including
	// the ICMP header). Default: 56 bytes (matching the standard `ping`
	// command, which produces 64-byte packets with the 8-byte ICMP header).
	PayloadSize int `json:"payload_size"`

	// Privileged controls whether to use raw ICMP sockets (true) or
	// unprivileged UDP (false). Raw sockets require CAP_NET_RAW or root.
	// Default: true.
	Privileged bool `json:"privileged"`
}

// DefaultConfig returns a PingConfig with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Count:       5,
		Interval:    200 * time.Millisecond,
		PayloadSize: 56,
		Privileged:  true,
	}
}

// Metrics holds the results of a single ping execution.
//
// These are embedded in the canary.Result.Metrics field as JSON and later
// parsed by the Metrics Processor into individual Prometheus metrics.
type Metrics struct {
	// RTT values in milliseconds.
	RTTMin    float64 `json:"rtt_min_ms"`
	RTTAvg    float64 `json:"rtt_avg_ms"`
	RTTMax    float64 `json:"rtt_max_ms"`
	RTTStdDev float64 `json:"rtt_stddev_ms"`

	// PacketLoss as a ratio from 0.0 (no loss) to 1.0 (100% loss).
	PacketLoss float64 `json:"packet_loss_ratio"`

	// Jitter is the mean absolute deviation of consecutive RTT differences,
	// in milliseconds. Measures path stability.
	Jitter float64 `json:"jitter_ms"`

	// Packet counts for the execution.
	PacketsSent int `json:"packets_sent"`
	PacketsRecv int `json:"packets_recv"`
}
