// Package processor implements the NetVantage Metrics Processor.
//
// The processor sits between the transport layer and Prometheus:
//
//	Agent → NATS JetStream → Processor → Prometheus
//
// For each test result consumed from the transport, the processor:
//  1. Deserializes the JSON result
//  2. Extracts canary-type-specific metrics
//  3. Records them as Prometheus gauges/counters/histograms
//  4. Exposes them on an HTTP /metrics endpoint for Prometheus to scrape
//
// The processor is stateless — all state lives in Prometheus. It can be
// horizontally scaled by running multiple instances with NATS consumer groups.
package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/netvantage/netvantage/internal/agent/canary"
	"github.com/netvantage/netvantage/internal/transport"
)

// Processor consumes test results from the transport and exposes them as
// Prometheus metrics.
type Processor struct {
	consumer transport.Consumer
	logger   *slog.Logger
	registry *prometheus.Registry

	// Ping metrics.
	pingRTT        *prometheus.GaugeVec
	pingPacketLoss *prometheus.GaugeVec
	pingJitter     *prometheus.GaugeVec

	// General metrics.
	resultsProcessed *prometheus.CounterVec
	processingErrors *prometheus.CounterVec
}

// New creates a new Metrics Processor.
func New(consumer transport.Consumer, logger *slog.Logger) *Processor {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())

	p := &Processor{
		consumer: consumer,
		logger:   logger,
		registry: reg,

		pingRTT: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_ping_rtt_seconds",
			Help: "Ping round-trip time in seconds",
		}, []string{"target", "pop", "agent_id", "stat"}),

		pingPacketLoss: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_ping_packet_loss_ratio",
			Help: "Ping packet loss as ratio (0.0–1.0)",
		}, []string{"target", "pop", "agent_id"}),

		pingJitter: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_ping_jitter_seconds",
			Help: "Ping jitter (mean absolute RTT deviation) in seconds",
		}, []string{"target", "pop", "agent_id"}),

		resultsProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netvantage_processor_results_total",
			Help: "Total test results processed",
		}, []string{"test_type", "pop"}),

		processingErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netvantage_processor_errors_total",
			Help: "Total errors processing test results",
		}, []string{"test_type", "error_type"}),
	}

	reg.MustRegister(p.pingRTT)
	reg.MustRegister(p.pingPacketLoss)
	reg.MustRegister(p.pingJitter)
	reg.MustRegister(p.resultsProcessed)
	reg.MustRegister(p.processingErrors)

	return p
}

// Run starts consuming from the transport and serving metrics. Blocks until
// ctx is cancelled.
func (p *Processor) Run(ctx context.Context, metricsAddr string) error {
	// Start Prometheus metrics HTTP server.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:         metricsAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		p.logger.Info("processor metrics server starting", "addr", metricsAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.logger.Error("metrics server error", "error", err)
		}
	}()

	// Subscribe to test result topics.
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.consumer.Subscribe(ctx, "netvantage.ping.results", p.handlePingResult)
	}()

	// TODO(M4): Subscribe to netvantage.dns.results
	// TODO(M6): Subscribe to netvantage.http.results
	// TODO(M7): Subscribe to netvantage.traceroute.results

	// Block until shutdown.
	select {
	case <-ctx.Done():
		p.logger.Info("processor shutting down")
	case err := <-errCh:
		if err != nil {
			p.logger.Error("subscription error", "error", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// handlePingResult processes a single ping test result from the transport.
func (p *Processor) handlePingResult(_ context.Context, msg []byte) error {
	var result canary.Result
	if err := json.Unmarshal(msg, &result); err != nil {
		p.processingErrors.WithLabelValues("ping", "unmarshal").Inc()
		p.logger.Error("failed to unmarshal ping result", "error", err)
		return nil // Don't redeliver — malformed messages won't fix themselves.
	}

	p.resultsProcessed.WithLabelValues("ping", result.POPName).Inc()

	var metrics pingMetrics
	if err := json.Unmarshal(result.Metrics, &metrics); err != nil {
		p.processingErrors.WithLabelValues("ping", "metrics_unmarshal").Inc()
		p.logger.Error("failed to unmarshal ping metrics", "error", err, "test_id", result.TestID)
		return nil
	}

	labels := []string{result.Target, result.POPName, result.AgentID}

	// Convert milliseconds to seconds for Prometheus conventions.
	p.pingRTT.WithLabelValues(append(labels, "min")...).Set(metrics.RTTMin / 1000.0)
	p.pingRTT.WithLabelValues(append(labels, "avg")...).Set(metrics.RTTAvg / 1000.0)
	p.pingRTT.WithLabelValues(append(labels, "max")...).Set(metrics.RTTMax / 1000.0)
	p.pingRTT.WithLabelValues(append(labels, "stddev")...).Set(metrics.RTTStdDev / 1000.0)

	p.pingPacketLoss.WithLabelValues(labels...).Set(metrics.PacketLoss)
	p.pingJitter.WithLabelValues(labels...).Set(metrics.Jitter / 1000.0)

	p.logger.Debug("processed ping result",
		"target", result.Target,
		"pop", result.POPName,
		"rtt_avg_ms", fmt.Sprintf("%.2f", metrics.RTTAvg),
		"packet_loss", fmt.Sprintf("%.1f%%", metrics.PacketLoss*100),
	)

	return nil
}

// pingMetrics mirrors the Metrics struct from the ping canary package.
// Defined here to avoid an import dependency from processor → agent.
type pingMetrics struct {
	RTTMin      float64 `json:"rtt_min_ms"`
	RTTAvg      float64 `json:"rtt_avg_ms"`
	RTTMax      float64 `json:"rtt_max_ms"`
	RTTStdDev   float64 `json:"rtt_stddev_ms"`
	PacketLoss  float64 `json:"packet_loss_ratio"`
	Jitter      float64 `json:"jitter_ms"`
	PacketsSent int     `json:"packets_sent"`
	PacketsRecv int     `json:"packets_recv"`
}
