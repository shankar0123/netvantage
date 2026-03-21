// NetVantage Metrics Processor
//
// The processor consumes test results from the transport layer (NATS/Kafka),
// computes derived metrics, and exposes them as Prometheus metrics on /metrics.
//
// Usage:
//
//	netvantage-processor
//	NETVANTAGE_PROCESSOR_ADDR=:9091 netvantage-processor
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/netvantage/netvantage/internal/processor"
	natsTransport "github.com/netvantage/netvantage/internal/transport/nats"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Transport connection.
	natsURL := os.Getenv("NETVANTAGE_NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	transport, err := natsTransport.New(natsTransport.Config{
		URL: natsURL,
	}, logger)
	if err != nil {
		logger.Error("failed to connect to transport", "error", err)
		os.Exit(1)
	}
	defer transport.Close()

	// Metrics HTTP address.
	metricsAddr := os.Getenv("NETVANTAGE_PROCESSOR_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9091"
	}

	p := processor.New(transport, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("metrics processor starting",
		"nats_url", natsURL,
		"metrics_addr", metricsAddr,
	)

	if err := p.Run(ctx, metricsAddr); err != nil {
		logger.Error("processor exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("metrics processor stopped")
}
