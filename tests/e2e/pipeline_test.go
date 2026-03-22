//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
	"github.com/netvantage/netvantage/internal/processor"
	natstransport "github.com/netvantage/netvantage/internal/transport/nats"
)

// TestFullPipeline_PingThroughNATS exercises the complete data path:
//
//	Agent publishes ping result → NATS JetStream → Processor consumes →
//	Prometheus metrics updated → /metrics endpoint verifiable
//
// This is the M3 integration test deliverable: "full pipeline from agent →
// NATS → processor → Prometheus query". It uses a real NATS container and
// the real Processor code — no mocks.
func TestFullPipeline_PingThroughNATS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// --- Start NATS ---
	_, natsURL := startNATS(ctx, t)

	// Create separate publisher and consumer transports (simulating agent + processor).
	publisher, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create publisher transport: %v", err)
	}
	defer publisher.Close()

	consumer, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create consumer transport: %v", err)
	}
	defer consumer.Close()

	// --- Start Processor ---
	proc := processor.New(consumer, testLogger())

	// Find a free port for the processor's metrics server.
	metricsAddr := freePort(t)

	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	procErrCh := make(chan error, 1)
	go func() {
		procErrCh <- proc.Run(procCtx, metricsAddr)
	}()

	// Wait for the metrics server to be ready.
	waitForHTTP(t, fmt.Sprintf("http://%s/healthz", metricsAddr), 10*time.Second)

	// --- Simulate Agent Publishing a Ping Result ---
	pingMetrics := map[string]interface{}{
		"rtt_min_ms":    1.5,
		"rtt_avg_ms":    5.0,
		"rtt_max_ms":    10.0,
		"rtt_stddev_ms": 2.0,
		"packet_loss":   0.0,
		"jitter_ms":     1.0,
		"packets_sent":  5,
		"packets_recv":  5,
	}
	metricsJSON, _ := json.Marshal(pingMetrics)

	result := canary.Result{
		TestID:     "e2e-ping-1",
		AgentID:    "agent-e2e-01",
		POPName:    "us-east-1-aws",
		TestType:   "ping",
		Target:     "8.8.8.8",
		Timestamp:  time.Now().UTC(),
		DurationMS: 1200,
		Success:    true,
		Metrics:    metricsJSON,
	}
	resultJSON, _ := json.Marshal(result)

	if err := publisher.Publish(ctx, "netvantage.ping.results", resultJSON); err != nil {
		t.Fatalf("agent publish failed: %v", err)
	}

	// --- Verify Metrics Appear on /metrics Endpoint ---
	// The processor needs a moment to consume from NATS and update metrics.
	var found bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", metricsAddr))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()

		body := make([]byte, 64*1024)
		n, _ := resp.Body.Read(body)
		bodyStr := string(body[:n])

		// Verify key ping metrics are present.
		checks := []string{
			`netvantage_ping_rtt_seconds{agent_id="agent-e2e-01"`,
			`netvantage_ping_packet_loss_ratio{agent_id="agent-e2e-01"`,
			`netvantage_processor_results_total{pop="us-east-1-aws",test_type="ping"}`,
		}

		allFound := true
		for _, check := range checks {
			if !strings.Contains(bodyStr, check) {
				allFound = false
				break
			}
		}

		if allFound {
			found = true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	if !found {
		t.Error("ping metrics did not appear on /metrics endpoint within timeout")
	}

	// Shut down processor cleanly.
	procCancel()
	select {
	case err := <-procErrCh:
		if err != nil && err != context.Canceled {
			t.Logf("processor shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Log("warning: processor did not shut down within 5s")
	}
}

// TestFullPipeline_DNSThroughNATS verifies the DNS canary result pipeline.
func TestFullPipeline_DNSThroughNATS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, natsURL := startNATS(ctx, t)

	publisher, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create publisher transport: %v", err)
	}
	defer publisher.Close()

	consumer, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create consumer transport: %v", err)
	}
	defer consumer.Close()

	proc := processor.New(consumer, testLogger())
	metricsAddr := freePort(t)

	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	go func() { _ = proc.Run(procCtx, metricsAddr) }()
	waitForHTTP(t, fmt.Sprintf("http://%s/healthz", metricsAddr), 10*time.Second)

	// DNS result with resolver data.
	dnsMetrics := map[string]interface{}{
		"record_type":          "A",
		"avg_resolution_ms":    12.5,
		"resolvers": []map[string]interface{}{
			{
				"resolver":          "8.8.8.8",
				"resolution_time_ms": 12.5,
				"response_code":     "NOERROR",
				"values":            []string{"93.184.216.34"},
			},
		},
	}
	metricsJSON, _ := json.Marshal(dnsMetrics)

	result := canary.Result{
		TestID:     "e2e-dns-1",
		AgentID:    "agent-e2e-01",
		POPName:    "eu-west-1-aws",
		TestType:   "dns",
		Target:     "example.com",
		Timestamp:  time.Now().UTC(),
		DurationMS: 15,
		Success:    true,
		Metrics:    metricsJSON,
	}
	resultJSON, _ := json.Marshal(result)

	if err := publisher.Publish(ctx, "netvantage.dns.results", resultJSON); err != nil {
		t.Fatalf("publish DNS result failed: %v", err)
	}

	// Verify DNS metrics appear.
	var found bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", metricsAddr))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body := make([]byte, 64*1024)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()

		if strings.Contains(string(body[:n]), `netvantage_dns_resolution_seconds{`) {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !found {
		t.Error("DNS metrics did not appear on /metrics endpoint within timeout")
	}

	procCancel()
}

// TestFullPipeline_HTTPThroughNATS verifies the HTTP canary result pipeline.
func TestFullPipeline_HTTPThroughNATS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, natsURL := startNATS(ctx, t)

	publisher, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create publisher transport: %v", err)
	}
	defer publisher.Close()

	consumer, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create consumer transport: %v", err)
	}
	defer consumer.Close()

	proc := processor.New(consumer, testLogger())
	metricsAddr := freePort(t)

	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	go func() { _ = proc.Run(procCtx, metricsAddr) }()
	waitForHTTP(t, fmt.Sprintf("http://%s/healthz", metricsAddr), 10*time.Second)

	httpMetrics := map[string]interface{}{
		"dns_ms":          5.2,
		"tcp_ms":          10.1,
		"tls_ms":          25.3,
		"ttfb_ms":         45.0,
		"total_ms":        120.5,
		"status_code":     200,
		"cert_expiry_days": 45,
	}
	metricsJSON, _ := json.Marshal(httpMetrics)

	result := canary.Result{
		TestID:     "e2e-http-1",
		AgentID:    "agent-e2e-01",
		POPName:    "us-west-2-aws",
		TestType:   "http",
		Target:     "https://example.com",
		Timestamp:  time.Now().UTC(),
		DurationMS: 120,
		Success:    true,
		Metrics:    metricsJSON,
	}
	resultJSON, _ := json.Marshal(result)

	if err := publisher.Publish(ctx, "netvantage.http.results", resultJSON); err != nil {
		t.Fatalf("publish HTTP result failed: %v", err)
	}

	var found bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", metricsAddr))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body := make([]byte, 64*1024)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()

		bodyStr := string(body[:n])
		if strings.Contains(bodyStr, `netvantage_http_duration_seconds{`) &&
			strings.Contains(bodyStr, `netvantage_http_status_code{`) {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !found {
		t.Error("HTTP metrics did not appear on /metrics endpoint within timeout")
	}

	procCancel()
}

// TestFullPipeline_MultipleCanaryTypes verifies that the processor correctly
// handles multiple canary types arriving on different topics simultaneously.
// This exercises NATS's multi-subject subscription and the processor's
// concurrent handler dispatch.
func TestFullPipeline_MultipleCanaryTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, natsURL := startNATS(ctx, t)

	publisher, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create publisher transport: %v", err)
	}
	defer publisher.Close()

	consumer, err := natstransport.New(natstransport.Config{URL: natsURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create consumer transport: %v", err)
	}
	defer consumer.Close()

	proc := processor.New(consumer, testLogger())
	metricsAddr := freePort(t)

	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	go func() { _ = proc.Run(procCtx, metricsAddr) }()
	waitForHTTP(t, fmt.Sprintf("http://%s/healthz", metricsAddr), 10*time.Second)

	// Publish ping result.
	pingM, _ := json.Marshal(map[string]interface{}{
		"rtt_min_ms": 1.0, "rtt_avg_ms": 3.0, "rtt_max_ms": 5.0,
		"rtt_stddev_ms": 1.0, "packet_loss": 0.0, "jitter_ms": 0.5,
		"packets_sent": 5, "packets_recv": 5,
	})
	pingResult, _ := json.Marshal(canary.Result{
		TestID: "e2e-multi-ping", AgentID: "agent-e2e-01", POPName: "us-east-1-aws",
		TestType: "ping", Target: "1.1.1.1", Timestamp: time.Now().UTC(),
		DurationMS: 50, Success: true, Metrics: pingM,
	})

	// Publish DNS result.
	dnsM, _ := json.Marshal(map[string]interface{}{
		"record_type": "A", "avg_resolution_ms": 8.0,
		"resolvers": []map[string]interface{}{
			{"resolver": "1.1.1.1", "resolution_time_ms": 8.0, "response_code": "NOERROR", "values": []string{"93.184.216.34"}},
		},
	})
	dnsResult, _ := json.Marshal(canary.Result{
		TestID: "e2e-multi-dns", AgentID: "agent-e2e-01", POPName: "us-east-1-aws",
		TestType: "dns", Target: "example.com", Timestamp: time.Now().UTC(),
		DurationMS: 10, Success: true, Metrics: dnsM,
	})

	// Publish both.
	if err := publisher.Publish(ctx, "netvantage.ping.results", pingResult); err != nil {
		t.Fatalf("publish ping failed: %v", err)
	}
	if err := publisher.Publish(ctx, "netvantage.dns.results", dnsResult); err != nil {
		t.Fatalf("publish dns failed: %v", err)
	}

	// Verify both metric families appear.
	var pingFound, dnsFound bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", metricsAddr))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body := make([]byte, 128*1024)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()

		bodyStr := string(body[:n])
		if strings.Contains(bodyStr, `netvantage_ping_rtt_seconds{`) {
			pingFound = true
		}
		if strings.Contains(bodyStr, `netvantage_dns_resolution_seconds{`) {
			dnsFound = true
		}

		if pingFound && dnsFound {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !pingFound {
		t.Error("ping metrics not found after publishing to multi-canary pipeline")
	}
	if !dnsFound {
		t.Error("DNS metrics not found after publishing to multi-canary pipeline")
	}

	procCancel()
}

// --- Helpers ---

// freePort finds and returns a free localhost address (host:port).
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// waitForHTTP polls a URL until it returns 200 or the timeout expires.
func waitForHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", url)
}
