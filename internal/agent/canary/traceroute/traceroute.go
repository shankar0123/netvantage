// Package traceroute implements the traceroute canary for NetVantage.
//
// The traceroute canary executes hop-by-hop path probing to configured targets
// using mtr (default) or scamper (optional, for Paris traceroute). It collects
// per-hop RTT, packet loss, ASN, hostname, and builds an AS path for use in
// BGP + traceroute correlation (M8).
//
// mtr is invoked with `--json` output for structured parsing. scamper uses
// `warts2json` for structured output.
//
// Requires CAP_NET_RAW capability (or running as root) for raw socket probing.
package traceroute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
)

// Canary implements the canary.Canary interface for traceroute tests.
type Canary struct{}

// New creates a new Traceroute canary instance.
func New() *Canary {
	return &Canary{}
}

// Type returns "traceroute".
func (c *Canary) Type() string {
	return "traceroute"
}

// Validate checks that traceroute-specific configuration is valid.
func (c *Canary) Validate(config json.RawMessage) error {
	if len(config) == 0 || string(config) == "null" {
		return nil // Defaults are fine.
	}
	var cfg Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("traceroute: invalid config: %w", err)
	}

	if cfg.Backend != "" && cfg.Backend != "mtr" && cfg.Backend != "scamper" {
		return fmt.Errorf("traceroute: backend must be 'mtr' or 'scamper', got %q", cfg.Backend)
	}
	if cfg.Cycles < 0 {
		return fmt.Errorf("traceroute: cycles must be >= 0, got %d", cfg.Cycles)
	}
	if cfg.MaxHops < 0 || cfg.MaxHops > 255 {
		return fmt.Errorf("traceroute: max_hops must be 0–255, got %d", cfg.MaxHops)
	}
	if cfg.Protocol != "" {
		p := strings.ToLower(cfg.Protocol)
		if p != "icmp" && p != "udp" && p != "tcp" {
			return fmt.Errorf("traceroute: protocol must be 'icmp', 'udp', or 'tcp', got %q", cfg.Protocol)
		}
	}
	if cfg.IntervalSec < 0 {
		return fmt.Errorf("traceroute: interval_sec must be >= 0, got %f", cfg.IntervalSec)
	}
	return nil
}

// Execute runs a traceroute to the target defined in the test.
func (c *Canary) Execute(ctx context.Context, test canary.TestDefinition) (*canary.Result, error) {
	cfg := DefaultConfig()
	if len(test.Config) > 0 && string(test.Config) != "null" {
		if err := json.Unmarshal(test.Config, &cfg); err != nil {
			return nil, fmt.Errorf("traceroute: parse config: %w", err)
		}
	}

	// Apply defaults for zero-valued fields after unmarshal.
	cfg = mergeDefaults(cfg)

	start := time.Now()

	var hops []Hop
	var err error

	switch cfg.Backend {
	case "mtr":
		hops, err = runMTR(ctx, test.Target, cfg, test.Timeout)
	case "scamper":
		hops, err = runScamper(ctx, test.Target, cfg, test.Timeout)
	default:
		return nil, fmt.Errorf("traceroute: unsupported backend %q", cfg.Backend)
	}

	elapsed := time.Since(start)

	if err != nil {
		// Return a failed result with whatever data we collected.
		metrics := Metrics{
			Backend: cfg.Backend,
			Hops:    hops,
			TotalMS: float64(elapsed.Milliseconds()),
		}
		metricsJSON, _ := json.Marshal(metrics)
		return &canary.Result{
			TestID:     test.ID,
			TestType:   "traceroute",
			Target:     test.Target,
			Timestamp:  start.UTC(),
			DurationMS: float64(elapsed.Milliseconds()),
			Success:    false,
			Metrics:    metricsJSON,
			Error:      err.Error(),
		}, nil
	}

	// Build AS path from hops.
	asPath := buildASPath(hops)

	// Determine if we reached the target.
	reachedTarget := didReachTarget(hops, test.Target)

	metrics := Metrics{
		Backend:       cfg.Backend,
		Hops:          hops,
		HopCount:      len(hops),
		ReachedTarget: reachedTarget,
		ASPath:        asPath,
		TotalMS:       float64(elapsed.Milliseconds()),
	}

	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("traceroute: marshal metrics: %w", err)
	}

	return &canary.Result{
		TestID:     test.ID,
		TestType:   "traceroute",
		Target:     test.Target,
		Timestamp:  start.UTC(),
		DurationMS: float64(elapsed.Milliseconds()),
		Success:    reachedTarget,
		Metrics:    metricsJSON,
	}, nil
}

// mergeDefaults applies default values for zero-valued config fields.
func mergeDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.Backend == "" {
		cfg.Backend = defaults.Backend
	}
	if cfg.Cycles == 0 {
		cfg.Cycles = defaults.Cycles
	}
	if cfg.MaxHops == 0 {
		cfg.MaxHops = defaults.MaxHops
	}
	if cfg.PacketSize == 0 {
		cfg.PacketSize = defaults.PacketSize
	}
	if cfg.Protocol == "" {
		cfg.Protocol = defaults.Protocol
	}
	if cfg.IntervalSec == 0 {
		cfg.IntervalSec = defaults.IntervalSec
	}
	if cfg.ResolveASN == nil {
		cfg.ResolveASN = defaults.ResolveASN
	}
	if cfg.ResolveHostnames == nil {
		cfg.ResolveHostnames = defaults.ResolveHostnames
	}
	return cfg
}

// runMTR executes `mtr --json` and parses the output into Hops.
func runMTR(ctx context.Context, target string, cfg Config, timeout time.Duration) ([]Hop, error) {
	args := buildMTRArgs(target, cfg)

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "mtr", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mtr execution failed: %w (stderr: %s)", err, stderr.String())
	}

	return parseMTRJSON(stdout.Bytes())
}

// buildMTRArgs constructs the mtr command-line arguments.
func buildMTRArgs(target string, cfg Config) []string {
	args := []string{
		"--json",          // JSON output
		"--report",        // Report mode (non-interactive)
		"-c", strconv.Itoa(cfg.Cycles),
		"-m", strconv.Itoa(cfg.MaxHops),
		"-s", strconv.Itoa(cfg.PacketSize),
		"-i", fmt.Sprintf("%.2f", cfg.IntervalSec),
	}

	if cfg.ResolveHostnames != nil && !*cfg.ResolveHostnames {
		args = append(args, "--no-dns")
	}

	switch strings.ToLower(cfg.Protocol) {
	case "tcp":
		args = append(args, "--tcp")
		if cfg.Port > 0 {
			args = append(args, "-P", strconv.Itoa(cfg.Port))
		}
	case "udp":
		args = append(args, "--udp")
		if cfg.Port > 0 {
			args = append(args, "-P", strconv.Itoa(cfg.Port))
		}
	// icmp is the default, no flag needed.
	}

	args = append(args, target)
	return args
}

// mtrReport is the top-level JSON output from `mtr --json`.
type mtrReport struct {
	Report struct {
		MTR struct {
			Src        string `json:"src"`
			Dst        string `json:"dst"`
			TOS        int    `json:"tos"`
			Tests      int    `json:"tests"`
			PacketSize string `json:"psize"`
			BitPattern string `json:"bitpattern"`
		} `json:"mtr"`
		Hops []mtrHop `json:"hubs"`
	} `json:"report"`
}

// mtrHop is a single hop in the mtr JSON output.
type mtrHop struct {
	Count int     `json:"count"`
	Host  string  `json:"host"`
	Loss  float64 `json:"Loss%"`
	Sent  int     `json:"Snt"`
	Last  float64 `json:"Last"`
	Avg   float64 `json:"Avg"`
	Best  float64 `json:"Best"`
	Worst float64 `json:"Wrst"`
	StDev float64 `json:"StDev"`
}

// parseMTRJSON parses the JSON output from `mtr --json`.
func parseMTRJSON(data []byte) ([]Hop, error) {
	var report mtrReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse mtr json: %w", err)
	}

	hops := make([]Hop, 0, len(report.Report.Hops))
	for i, h := range report.Report.Hops {
		hop := Hop{
			HopNumber:  i + 1,
			RTTMin:     h.Best,
			RTTAvg:     h.Avg,
			RTTMax:     h.Worst,
			RTTStdDev:  h.StDev,
			PacketLoss: h.Loss / 100.0, // mtr reports percentage, we want ratio.
			Sent:       h.Sent,
		}

		// mtr uses "???" for unresponsive hops.
		if h.Host != "???" {
			// Check if host is an IP or a hostname.
			if ip := net.ParseIP(h.Host); ip != nil {
				hop.IP = h.Host
			} else {
				hop.Hostname = h.Host
				// Try to resolve hostname → IP for ASN lookup.
				ips, err := net.LookupHost(h.Host)
				if err == nil && len(ips) > 0 {
					hop.IP = ips[0]
				}
			}
		}

		// Calculate received count from loss and sent.
		if h.Sent > 0 {
			hop.Received = h.Sent - int(float64(h.Sent)*hop.PacketLoss+0.5)
		}

		hops = append(hops, hop)
	}

	return hops, nil
}

// runScamper executes scamper and parses the output into Hops.
// scamper uses `warts` binary format; we invoke with `-O json` for JSON output.
func runScamper(ctx context.Context, target string, cfg Config, timeout time.Duration) ([]Hop, error) {
	args := buildScamperArgs(target, cfg)

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "scamper", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scamper execution failed: %w (stderr: %s)", err, stderr.String())
	}

	return parseScamperJSON(stdout.Bytes())
}

// buildScamperArgs constructs scamper command-line arguments.
func buildScamperArgs(target string, cfg Config) []string {
	// scamper -O json -c "trace -P icmp -q <cycles> -m <maxhops>" -i <target>
	traceCmd := fmt.Sprintf("trace -P %s -q %d -m %d",
		strings.ToLower(cfg.Protocol),
		cfg.Cycles,
		cfg.MaxHops,
	)

	if cfg.Port > 0 && (cfg.Protocol == "tcp" || cfg.Protocol == "udp") {
		traceCmd += fmt.Sprintf(" -d %d", cfg.Port)
	}

	return []string{
		"-O", "json",
		"-c", traceCmd,
		"-i", target,
	}
}

// scamperTrace is the JSON output from scamper's trace.
type scamperTrace struct {
	Type   string       `json:"type"`
	Src    string       `json:"src"`
	Dst    string       `json:"dst"`
	Hops   []scamperHop `json:"hops"`
	HopCnt int          `json:"hop_count"`
}

// scamperHop represents a single hop in scamper's JSON output.
type scamperHop struct {
	Addr     string           `json:"addr"`
	Name     string           `json:"name,omitempty"`
	ProbeTTL int              `json:"probe_ttl"`
	Probes   []scamperProbe   `json:"probes,omitempty"`
	RTT      float64          `json:"rtt,omitempty"`
}

type scamperProbe struct {
	RTT    float64 `json:"rtt"`
	Reply  int     `json:"reply"`
}

// parseScamperJSON parses JSON output from scamper.
func parseScamperJSON(data []byte) ([]Hop, error) {
	// scamper outputs one JSON object per line.
	var trace scamperTrace
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		if obj["type"] == "trace" {
			if err := json.Unmarshal(line, &trace); err != nil {
				return nil, fmt.Errorf("parse scamper trace: %w", err)
			}
			break
		}
	}

	if trace.Type == "" {
		return nil, fmt.Errorf("no trace object found in scamper output")
	}

	hops := make([]Hop, 0, len(trace.Hops))
	for _, h := range trace.Hops {
		hop := Hop{
			HopNumber: h.ProbeTTL,
			IP:        h.Addr,
			Hostname:  h.Name,
		}

		if len(h.Probes) > 0 {
			var sum, min, max float64
			min = h.Probes[0].RTT
			received := 0
			for _, p := range h.Probes {
				if p.Reply > 0 {
					received++
					sum += p.RTT
					if p.RTT < min {
						min = p.RTT
					}
					if p.RTT > max {
						max = p.RTT
					}
				}
			}
			hop.Sent = len(h.Probes)
			hop.Received = received
			if received > 0 {
				hop.RTTMin = min
				hop.RTTMax = max
				hop.RTTAvg = sum / float64(received)
			}
			hop.PacketLoss = 1.0 - float64(received)/float64(len(h.Probes))
		}

		hops = append(hops, hop)
	}

	return hops, nil
}

// buildASPath extracts the ordered, deduplicated list of ASNs from the hop list.
func buildASPath(hops []Hop) []int {
	var path []int
	for _, h := range hops {
		if h.ASN == 0 {
			continue
		}
		// Deduplicate consecutive identical ASNs.
		if len(path) == 0 || path[len(path)-1] != h.ASN {
			path = append(path, h.ASN)
		}
	}
	return path
}

// didReachTarget checks if the last responding hop matches the target IP.
func didReachTarget(hops []Hop, target string) bool {
	if len(hops) == 0 {
		return false
	}

	// Resolve the target to IPs for comparison.
	targetIPs := make(map[string]bool)
	if ip := net.ParseIP(target); ip != nil {
		targetIPs[ip.String()] = true
	} else {
		ips, err := net.LookupHost(target)
		if err == nil {
			for _, ip := range ips {
				targetIPs[ip] = true
			}
		}
	}

	// Check the last hop with a valid IP.
	for i := len(hops) - 1; i >= 0; i-- {
		if hops[i].IP != "" {
			return targetIPs[hops[i].IP]
		}
	}
	return false
}
