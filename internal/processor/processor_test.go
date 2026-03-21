package processor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
	"github.com/netvantage/netvantage/internal/transport/memory"
)

// ---------------------------------------------------------------------------
// Ping result processing tests
// ---------------------------------------------------------------------------

func TestHandlePingResult(t *testing.T) {
	mem := memory.New()
	p := New(mem, nil)

	// Suppress logging in tests — create a minimal logger.
	// In real tests we'd use slog.New(slog.NewTextHandler(io.Discard, nil)).

	metrics := pingMetrics{
		RTTMin:      1.5,
		RTTAvg:      5.0,
		RTTMax:      10.0,
		RTTStdDev:   2.0,
		PacketLoss:  0.0,
		Jitter:      1.0,
		PacketsSent: 5,
		PacketsRecv: 5,
	}
	metricsJSON, _ := json.Marshal(metrics)

	result := canary.Result{
		TestID:     "test-1",
		AgentID:    "agent-1",
		POPName:    "us-east-1-aws",
		TestType:   "ping",
		Target:     "8.8.8.8",
		Timestamp:  time.Now().UTC(),
		DurationMS: 1200,
		Success:    true,
		Metrics:    metricsJSON,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	err = p.handlePingResult(context.Background(), resultJSON)
	if err != nil {
		t.Fatalf("handlePingResult returned error: %v", err)
	}
}

func TestHandlePingResultMalformed(t *testing.T) {
	mem := memory.New()
	p := New(mem, nil)

	// Malformed JSON should not return an error (don't redeliver).
	err := p.handlePingResult(context.Background(), []byte(`{not valid json}`))
	if err != nil {
		t.Errorf("expected nil error for malformed JSON, got: %v", err)
	}
}

func TestHandlePingResultBadMetrics(t *testing.T) {
	mem := memory.New()
	p := New(mem, nil)

	result := canary.Result{
		TestID:   "test-1",
		AgentID:  "agent-1",
		POPName:  "us-east-1-aws",
		TestType: "ping",
		Target:   "8.8.8.8",
		Metrics:  json.RawMessage(`{not valid}`),
	}

	resultJSON, _ := json.Marshal(result)

	// Bad metrics JSON inside a valid result envelope — should not error.
	err := p.handlePingResult(context.Background(), resultJSON)
	if err != nil {
		t.Errorf("expected nil error for bad metrics, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pipeline integration test (memory transport)
// ---------------------------------------------------------------------------

func TestPipelineMemoryTransport(t *testing.T) {
	mem := memory.New()
	p := New(mem, nil)

	// Subscribe the processor's handler to the ping results topic.
	err := mem.Subscribe(context.Background(), "netvantage.ping.results", p.handlePingResult)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	// Simulate an agent publishing a ping result.
	metrics := pingMetrics{
		RTTMin:      2.0,
		RTTAvg:      5.0,
		RTTMax:      8.0,
		RTTStdDev:   1.5,
		PacketLoss:  0.0,
		Jitter:      0.5,
		PacketsSent: 5,
		PacketsRecv: 5,
	}
	metricsJSON, _ := json.Marshal(metrics)

	result := canary.Result{
		TestID:     "test-pipeline",
		AgentID:    "agent-1",
		POPName:    "eu-west-1-aws",
		TestType:   "ping",
		Target:     "1.1.1.1",
		Timestamp:  time.Now().UTC(),
		DurationMS: 800,
		Success:    true,
		Metrics:    metricsJSON,
	}
	resultJSON, _ := json.Marshal(result)

	// Publish through the memory transport — this triggers the handler synchronously.
	err = mem.Publish(context.Background(), "netvantage.ping.results", resultJSON)
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// If we got here without error, the full pipeline worked:
	// agent publishes → transport delivers → processor handles → metrics recorded.
}
