// NetVantage Canary Agent
//
// The agent executes synthetic tests (ping, DNS, HTTP, traceroute) from
// Points of Presence and publishes results to the transport layer
// (NATS JetStream or Kafka).
//
// Usage:
//
//	netvantage-agent --config agent.yaml
//	netvantage-agent  # uses defaults + environment variables
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/netvantage/netvantage/internal/agent"
	"github.com/netvantage/netvantage/internal/agent/buffer"
	dnsCanary "github.com/netvantage/netvantage/internal/agent/canary/dns"
	httpCanary "github.com/netvantage/netvantage/internal/agent/canary/http"
	"github.com/netvantage/netvantage/internal/agent/canary/ping"
	trCanary "github.com/netvantage/netvantage/internal/agent/canary/traceroute"
	"github.com/netvantage/netvantage/internal/agent/config"
	natsTransport "github.com/netvantage/netvantage/internal/transport/nats"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := config.DefaultConfig()
	// TODO: Load config from YAML file and environment variables.
	// TODO: Generate agent_id on first run, persist to cache dir.

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Initialize transport.
	transport, err := natsTransport.New(natsTransport.Config{
		URL: cfg.Transport.NATS.URL,
	}, logger)
	if err != nil {
		logger.Error("failed to connect to transport", "error", err)
		os.Exit(1)
	}
	defer transport.Close()

	// Initialize local result buffer.
	buf := buffer.NewMemoryBuffer(cfg.Buffer.MaxSizeBytes)
	// TODO: Replace with disk-backed buffer (bbolt or SQLite) for production.

	// Create and configure the agent.
	a := agent.New(cfg, transport, buf, logger)

	// Register canary types.
	a.RegisterCanary(ping.New())
	a.RegisterCanary(dnsCanary.New())
	a.RegisterCanary(httpCanary.New())
	a.RegisterCanary(trCanary.New())

	// Run with graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("netvantage agent starting",
		"pop", cfg.POPName,
		"transport", cfg.Transport.Backend,
	)

	if err := a.Run(ctx); err != nil {
		logger.Error("agent exited with error", "error", err)
		os.Exit(1)
	}
}
