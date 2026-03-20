// NetVantage Metrics Processor
//
// The processor consumes test results from the transport layer (NATS/Kafka),
// computes derived metrics, and writes to Prometheus via remote_write.
//
// Usage:
//
//	netvantage-processor
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("metrics processor starting")

	// TODO(M3): Initialize transport consumer (NATS JetStream).
	// TODO(M3): Subscribe to netvantage.ping.results, parse, write to Prometheus.
	// TODO(M4): Subscribe to netvantage.dns.results.
	// TODO(M6): Subscribe to netvantage.http.results.
	// TODO(M7): Subscribe to netvantage.traceroute.results — flatten hop arrays.

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	logger.Info("metrics processor stopped")
}
