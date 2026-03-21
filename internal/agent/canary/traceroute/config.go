package traceroute

// Config holds traceroute canary-specific configuration.
//
// These fields are deserialized from the "config" JSON blob in a TestDefinition.
// All fields have sensible defaults — a minimal config only needs a target.
type Config struct {
	// Backend is the traceroute implementation to use: "mtr" or "scamper".
	// Default: "mtr". mtr is widely available and supports JSON output.
	// scamper supports Paris traceroute for load-balanced path detection.
	Backend string `json:"backend,omitempty"`

	// Cycles is the number of probing cycles (mtr -c). Higher values give
	// more statistically meaningful per-hop data but take longer.
	// Default: 10.
	Cycles int `json:"cycles,omitempty"`

	// MaxHops is the maximum number of hops to probe (mtr -m, TTL limit).
	// Default: 30.
	MaxHops int `json:"max_hops,omitempty"`

	// PacketSize is the size of probe packets in bytes.
	// Default: 64.
	PacketSize int `json:"packet_size,omitempty"`

	// Protocol is the probe protocol: "icmp", "udp", "tcp".
	// Default: "icmp".
	Protocol string `json:"protocol,omitempty"`

	// Port is the destination port for UDP/TCP probes.
	// Default: 80 for TCP, 33434 for UDP.
	Port int `json:"port,omitempty"`

	// Interval is the time between sending probe packets in seconds.
	// Default: 0.25 (mtr default).
	IntervalSec float64 `json:"interval_sec,omitempty"`

	// ResolveASN controls whether to resolve IP → ASN using Team Cymru DNS.
	// Default: true.
	ResolveASN *bool `json:"resolve_asn,omitempty"`

	// ResolveHostnames controls reverse DNS lookups for hop IPs.
	// Default: true.
	ResolveHostnames *bool `json:"resolve_hostnames,omitempty"`
}

// DefaultConfig returns sensible defaults for the traceroute canary.
func DefaultConfig() Config {
	resolveASN := true
	resolveHostnames := true
	return Config{
		Backend:          "mtr",
		Cycles:           10,
		MaxHops:          30,
		PacketSize:       64,
		Protocol:         "icmp",
		IntervalSec:      0.25,
		ResolveASN:       &resolveASN,
		ResolveHostnames: &resolveHostnames,
	}
}

// Hop represents a single hop in the traceroute path.
type Hop struct {
	// HopNumber is the TTL/hop position (1-indexed).
	HopNumber int `json:"hop_number"`

	// IP is the hop's IP address. Empty ("*") if the hop did not respond.
	IP string `json:"ip,omitempty"`

	// Hostname is the reverse DNS name of the hop IP.
	Hostname string `json:"hostname,omitempty"`

	// ASN is the Autonomous System Number for this hop's IP.
	// 0 if unknown or unresolvable.
	ASN int `json:"asn,omitempty"`

	// ASName is the organization name for the ASN.
	ASName string `json:"as_name,omitempty"`

	// RTT values in milliseconds across all probe cycles.
	RTTMin    float64 `json:"rtt_min_ms"`
	RTTAvg    float64 `json:"rtt_avg_ms"`
	RTTMax    float64 `json:"rtt_max_ms"`
	RTTStdDev float64 `json:"rtt_stddev_ms,omitempty"`

	// PacketLoss as a ratio from 0.0 to 1.0 at this hop.
	PacketLoss float64 `json:"packet_loss_ratio"`

	// Sent is the number of probes sent to this hop.
	Sent int `json:"sent"`

	// Received is the number of probes that received a response.
	Received int `json:"received,omitempty"`
}

// Metrics holds the full traceroute result.
type Metrics struct {
	// Backend is the tool used: "mtr" or "scamper".
	Backend string `json:"backend"`

	// Hops is the ordered list of hops from source to target.
	Hops []Hop `json:"hops"`

	// HopCount is the number of hops to reach the target (or max hops).
	HopCount int `json:"hop_count"`

	// ReachedTarget indicates whether the final hop matches the target.
	ReachedTarget bool `json:"reached_target"`

	// ASPath is the ordered list of unique ASNs traversed.
	ASPath []int `json:"as_path,omitempty"`

	// TotalMS is the total time to execute the traceroute in milliseconds.
	TotalMS float64 `json:"total_ms"`
}
