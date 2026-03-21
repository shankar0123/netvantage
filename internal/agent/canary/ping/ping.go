// Package ping implements the ICMP Ping canary for NetVantage.
//
// The ping canary sends ICMP echo requests to configured targets and measures
// round-trip time, packet loss, and jitter. It is the foundational synthetic
// test — the simplest proof that the agent → transport → processor → Prometheus
// pipeline works end-to-end.
//
// Requires CAP_NET_RAW capability (or running as root) for raw ICMP sockets.
// Falls back to unprivileged UDP ping if privileged mode is unavailable.
package ping

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/netvantage/netvantage/internal/agent/canary"
)

// Canary implements the canary.Canary interface for ICMP ping tests.
type Canary struct{}

// New creates a new Ping canary instance.
func New() *Canary {
	return &Canary{}
}

// Type returns "ping".
func (c *Canary) Type() string {
	return "ping"
}

// Validate checks that ping-specific configuration is valid.
func (c *Canary) Validate(config json.RawMessage) error {
	if len(config) == 0 {
		return nil // Defaults are fine.
	}
	var cfg Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("ping: invalid config: %w", err)
	}
	if cfg.Count < 1 {
		return fmt.Errorf("ping: count must be >= 1, got %d", cfg.Count)
	}
	if cfg.PayloadSize < 0 || cfg.PayloadSize > 65535 {
		return fmt.Errorf("ping: payload_size must be 0–65535, got %d", cfg.PayloadSize)
	}
	return nil
}

// Execute runs an ICMP ping test against the target defined in the test.
func (c *Canary) Execute(ctx context.Context, test canary.TestDefinition) (*canary.Result, error) {
	cfg := DefaultConfig()
	if len(test.Config) > 0 {
		if err := json.Unmarshal(test.Config, &cfg); err != nil {
			return nil, fmt.Errorf("ping: parse config: %w", err)
		}
	}

	pinger, err := probing.NewPinger(test.Target)
	if err != nil {
		return nil, fmt.Errorf("ping: create pinger for %s: %w", test.Target, err)
	}

	pinger.Count = cfg.Count
	pinger.Interval = cfg.Interval
	pinger.Size = cfg.PayloadSize
	pinger.Timeout = test.Timeout
	pinger.SetPrivileged(cfg.Privileged)

	start := time.Now()

	// Run the ping, respecting context cancellation.
	done := make(chan error, 1)
	go func() {
		done <- pinger.Run()
	}()

	select {
	case <-ctx.Done():
		pinger.Stop()
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("ping: run failed for %s: %w", test.Target, err)
		}
	}

	elapsed := time.Since(start)
	stats := pinger.Statistics()

	metrics := Metrics{
		RTTMin:      float64(stats.MinRtt) / float64(time.Millisecond),
		RTTAvg:      float64(stats.AvgRtt) / float64(time.Millisecond),
		RTTMax:      float64(stats.MaxRtt) / float64(time.Millisecond),
		RTTStdDev:   float64(stats.StdDevRtt) / float64(time.Millisecond),
		PacketLoss:  stats.PacketLoss / 100.0, // pro-bing returns percentage, we want ratio 0–1.
		Jitter:      computeJitter(stats),
		PacketsSent: stats.PacketsSent,
		PacketsRecv: stats.PacketsRecv,
	}

	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("ping: marshal metrics: %w", err)
	}

	success := metrics.PacketLoss < 1.0 // At least one packet got through.

	return &canary.Result{
		TestID:     test.ID,
		TestType:   "ping",
		Target:     test.Target,
		Timestamp:  start.UTC(),
		DurationMS: float64(elapsed.Milliseconds()),
		Success:    success,
		Metrics:    metricsJSON,
	}, nil
}

// computeJitter calculates inter-packet delay variation from RTT samples.
// Uses mean absolute deviation of consecutive RTT differences.
func computeJitter(stats *probing.Statistics) float64 {
	rtts := stats.Rtts
	if len(rtts) < 2 {
		return 0
	}

	var totalDiff float64
	for i := 1; i < len(rtts); i++ {
		diff := math.Abs(float64(rtts[i]-rtts[i-1]) / float64(time.Millisecond))
		totalDiff += diff
	}
	return totalDiff / float64(len(rtts)-1)
}
