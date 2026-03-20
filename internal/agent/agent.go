// Package agent implements the NetVantage canary agent lifecycle.
//
// The agent lifecycle is:
//   1. Startup: load config from YAML/env, initialize transport and buffer
//   2. Registration: register with control plane (if configured)
//   3. Config sync: pull test definitions, cache locally for offline resilience
//   4. Execution loop: run canaries on configured intervals
//   5. Heartbeat: periodic liveness signal (independent of test execution)
//   6. Graceful shutdown: drain in-flight tests, flush buffer, deregister
//
// Key resilience patterns:
//   - Local result buffer: when transport is down, results go to disk
//   - Config caching: if control plane is unreachable, run from cached config
//   - Graceful degradation: one panicking canary never crashes the agent
//   - Heartbeat independence: heartbeats continue even if tests are failing
package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/netvantage/netvantage/internal/agent/buffer"
	"github.com/netvantage/netvantage/internal/agent/canary"
	"github.com/netvantage/netvantage/internal/agent/config"
	"github.com/netvantage/netvantage/internal/transport"
)

// Agent is the main canary agent process.
type Agent struct {
	cfg       *config.Config
	publisher transport.Publisher
	buffer    buffer.Buffer
	canaries  map[string]canary.Canary
	logger    *slog.Logger

	// cancel stops the agent's background goroutines.
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new Agent with the given configuration, transport, and buffer.
func New(cfg *config.Config, pub transport.Publisher, buf buffer.Buffer, logger *slog.Logger) *Agent {
	return &Agent{
		cfg:       cfg,
		publisher: pub,
		buffer:    buf,
		canaries:  make(map[string]canary.Canary),
		logger:    logger,
	}
}

// RegisterCanary adds a canary implementation to the agent. Called during
// startup before Run(). All canary types are compiled into the binary.
func (a *Agent) RegisterCanary(c canary.Canary) {
	a.canaries[c.Type()] = c
	a.logger.Info("registered canary", "type", c.Type())
}

// Run starts the agent lifecycle. It blocks until ctx is cancelled or a
// fatal error occurs. On context cancellation, it performs graceful shutdown.
func (a *Agent) Run(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	defer a.cancel()

	a.logger.Info("agent starting",
		"agent_id", a.cfg.AgentID,
		"pop", a.cfg.POPName,
		"transport", a.cfg.Transport.Backend,
	)

	// TODO(M4): Register with control plane and sync test definitions.
	// For M1–M3, tests come from static config.
	tests := a.loadTests()
	if len(tests) == 0 {
		a.logger.Warn("no test definitions loaded — agent is idle")
	}

	// Start heartbeat (independent of test execution).
	a.wg.Add(1)
	go a.heartbeatLoop(ctx)

	// Start buffer drain loop (replays buffered results when transport recovers).
	a.wg.Add(1)
	go a.bufferDrainLoop(ctx)

	// Start test execution loops.
	for _, test := range tests {
		c, ok := a.canaries[test.Type]
		if !ok {
			a.logger.Error("no canary registered for test type", "type", test.Type, "test_id", test.ID)
			continue
		}
		a.wg.Add(1)
		go a.testLoop(ctx, c, test)
	}

	// Block until shutdown signal.
	<-ctx.Done()
	a.logger.Info("agent shutting down, draining in-flight tests...")
	a.wg.Wait()
	a.logger.Info("agent stopped")
	return nil
}

// Stop triggers graceful shutdown of the agent.
func (a *Agent) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

// loadTests returns test definitions from static config. In M4+, this will
// also pull from the control plane and merge with cached config.
func (a *Agent) loadTests() []canary.TestDefinition {
	tests := make([]canary.TestDefinition, 0, len(a.cfg.StaticTests))
	for _, st := range a.cfg.StaticTests {
		tests = append(tests, canary.TestDefinition{
			ID:       st.ID,
			Type:     st.Type,
			Target:   st.Target,
			Interval: st.Interval,
			Timeout:  st.Timeout,
			Config:   st.Config,
		})
	}
	return tests
}

// testLoop runs a single canary on its configured interval. Panics in the
// canary are recovered — one failing test type never crashes the agent.
func (a *Agent) testLoop(ctx context.Context, c canary.Canary, test canary.TestDefinition) {
	defer a.wg.Done()

	ticker := time.NewTicker(test.Interval)
	defer ticker.Stop()

	a.logger.Info("starting test loop", "type", test.Type, "target", test.Target, "interval", test.Interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.executeTest(ctx, c, test)
		}
	}
}

// executeTest runs a single test execution with panic recovery and result publishing.
func (a *Agent) executeTest(ctx context.Context, c canary.Canary, test canary.TestDefinition) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("canary panic recovered",
				"type", test.Type,
				"target", test.Target,
				"panic", r,
			)
		}
	}()

	testCtx, cancel := context.WithTimeout(ctx, test.Timeout)
	defer cancel()

	result, err := c.Execute(testCtx, test)
	if err != nil {
		a.logger.Error("canary execution error",
			"type", test.Type,
			"target", test.Target,
			"error", err,
		)
		// Create a failure result so it's still recorded.
		result = &canary.Result{
			TestID:    test.ID,
			AgentID:   a.cfg.AgentID,
			POPName:   a.cfg.POPName,
			TestType:  test.Type,
			Target:    test.Target,
			Timestamp: time.Now().UTC(),
			Success:   false,
			Error:     err.Error(),
		}
	}

	// Populate agent metadata on every result.
	result.AgentID = a.cfg.AgentID
	result.POPName = a.cfg.POPName

	a.publishResult(ctx, test.Type, result)
}

// publishResult sends a result to the transport, falling back to the local
// buffer if the transport is unavailable.
func (a *Agent) publishResult(ctx context.Context, testType string, result *canary.Result) {
	topic := "netvantage." + testType + ".results"

	data, err := json.Marshal(result)
	if err != nil {
		a.logger.Error("failed to marshal result", "error", err)
		return
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Warn("transport publish failed, buffering locally",
			"topic", topic,
			"error", err,
		)
		if bufErr := a.buffer.Push(ctx, topic, data); bufErr != nil {
			a.logger.Error("local buffer push failed — result dropped",
				"error", bufErr,
			)
		}
	}
}

// heartbeatLoop sends periodic heartbeats to the transport. This runs
// independently of test execution — the control plane must always know
// the agent is alive, even if tests are failing.
func (a *Agent) heartbeatLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context) {
	hb := map[string]interface{}{
		"agent_id":  a.cfg.AgentID,
		"pop_name":  a.cfg.POPName,
		"timestamp": time.Now().UTC(),
		"status":    "online",
	}

	data, err := json.Marshal(hb)
	if err != nil {
		return
	}

	if err := a.publisher.Publish(ctx, "netvantage.agent.heartbeat", data); err != nil {
		a.logger.Debug("heartbeat publish failed", "error", err)
	}
}

// bufferDrainLoop periodically attempts to replay buffered results through
// the transport. Runs every 10 seconds.
func (a *Agent) bufferDrainLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.buffer.Len() == 0 {
				continue
			}
			a.logger.Info("draining buffer", "count", a.buffer.Len())
			_ = a.buffer.Drain(ctx, func(topic string, msg []byte) error {
				return a.publisher.Publish(ctx, topic, msg)
			})
		}
	}
}
