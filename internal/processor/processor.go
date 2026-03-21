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
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/netvantage/netvantage/internal/agent/canary"
	"github.com/netvantage/netvantage/internal/processor/correlation"
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

	// DNS metrics.
	dnsResolutionTime *prometheus.GaugeVec
	dnsResponseCode   *prometheus.GaugeVec

	// HTTP metrics.
	httpDuration   *prometheus.GaugeVec
	httpStatusCode *prometheus.GaugeVec
	httpCertExpiry *prometheus.GaugeVec

	// Traceroute metrics.
	tracerouteHopRTT        *prometheus.GaugeVec
	tracerouteHopLoss       *prometheus.GaugeVec
	tracerouteHopCount      *prometheus.GaugeVec
	traceroutePathChange    *prometheus.CounterVec
	tracerouteReachable     *prometheus.GaugeVec

	// Path change detection state: target@pop → last AS path hash.
	lastASPaths map[string]string

	// BGP + Traceroute correlation engine (M8).
	correlator *correlation.Engine

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

		dnsResolutionTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_dns_resolution_seconds",
			Help: "DNS resolution time in seconds",
		}, []string{"target", "pop", "agent_id", "resolver", "record_type"}),

		dnsResponseCode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_dns_response_code",
			Help: "DNS response code (1=this code observed, use label to identify code)",
		}, []string{"target", "pop", "agent_id", "resolver", "response_code"}),

		httpDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_http_duration_seconds",
			Help: "HTTP request phase duration in seconds",
		}, []string{"target", "pop", "agent_id", "phase"}),

		httpStatusCode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_http_status_code",
			Help: "HTTP response status code",
		}, []string{"target", "pop", "agent_id"}),

		httpCertExpiry: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_http_cert_expiry_days",
			Help: "Days until TLS certificate expires",
		}, []string{"target", "pop", "agent_id"}),

		tracerouteHopRTT: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_traceroute_hop_rtt_seconds",
			Help: "Traceroute per-hop round-trip time in seconds",
		}, []string{"target", "pop", "agent_id", "hop", "hop_ip", "hop_asn"}),

		tracerouteHopLoss: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_traceroute_hop_loss_ratio",
			Help: "Traceroute per-hop packet loss ratio (0.0–1.0)",
		}, []string{"target", "pop", "agent_id", "hop", "hop_ip"}),

		tracerouteHopCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_traceroute_hop_count",
			Help: "Number of hops to reach the target",
		}, []string{"target", "pop", "agent_id"}),

		traceroutePathChange: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netvantage_traceroute_path_change_total",
			Help: "Total number of AS path changes detected",
		}, []string{"target", "pop"}),

		tracerouteReachable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_traceroute_reachable",
			Help: "Whether the target was reached (1=yes, 0=no)",
		}, []string{"target", "pop", "agent_id"}),

		lastASPaths: make(map[string]string),

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
	reg.MustRegister(p.dnsResolutionTime)
	reg.MustRegister(p.dnsResponseCode)
	reg.MustRegister(p.httpDuration)
	reg.MustRegister(p.httpStatusCode)
	reg.MustRegister(p.httpCertExpiry)
	reg.MustRegister(p.tracerouteHopRTT)
	reg.MustRegister(p.tracerouteHopLoss)
	reg.MustRegister(p.tracerouteHopCount)
	reg.MustRegister(p.traceroutePathChange)
	reg.MustRegister(p.tracerouteReachable)
	reg.MustRegister(p.resultsProcessed)
	reg.MustRegister(p.processingErrors)

	// Initialize the BGP + Traceroute correlation engine.
	p.correlator = correlation.New(reg, logger)

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

	go func() {
		if err := p.consumer.Subscribe(ctx, "netvantage.dns.results", p.handleDNSResult); err != nil {
			p.logger.Error("dns subscription error", "error", err)
		}
	}()

	go func() {
		if err := p.consumer.Subscribe(ctx, "netvantage.http.results", p.handleHTTPResult); err != nil {
			p.logger.Error("http subscription error", "error", err)
		}
	}()

	go func() {
		if err := p.consumer.Subscribe(ctx, "netvantage.traceroute.results", p.handleTracerouteResult); err != nil {
			p.logger.Error("traceroute subscription error", "error", err)
		}
	}()

	// Subscribe to BGP path updates for correlation (M8).
	go func() {
		if err := p.consumer.Subscribe(ctx, "netvantage.bgp.paths", p.correlator.HandleBGPUpdate); err != nil {
			p.logger.Error("bgp paths subscription error", "error", err)
		}
	}()

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

// handleDNSResult processes a single DNS test result from the transport.
func (p *Processor) handleDNSResult(_ context.Context, msg []byte) error {
	var result canary.Result
	if err := json.Unmarshal(msg, &result); err != nil {
		p.processingErrors.WithLabelValues("dns", "unmarshal").Inc()
		p.logger.Error("failed to unmarshal dns result", "error", err)
		return nil
	}

	p.resultsProcessed.WithLabelValues("dns", result.POPName).Inc()

	var metrics dnsMetrics
	if err := json.Unmarshal(result.Metrics, &metrics); err != nil {
		p.processingErrors.WithLabelValues("dns", "metrics_unmarshal").Inc()
		p.logger.Error("failed to unmarshal dns metrics", "error", err, "test_id", result.TestID)
		return nil
	}

	for _, rr := range metrics.Resolvers {
		labels := []string{result.Target, result.POPName, result.AgentID, rr.Resolver, metrics.RecordType}
		p.dnsResolutionTime.WithLabelValues(labels...).Set(rr.ResolutionTimeMS / 1000.0)

		codeLabels := []string{result.Target, result.POPName, result.AgentID, rr.Resolver, rr.ResponseCode}
		p.dnsResponseCode.WithLabelValues(codeLabels...).Set(1)
	}

	p.logger.Debug("processed dns result",
		"target", result.Target,
		"pop", result.POPName,
		"record_type", metrics.RecordType,
		"avg_resolution_ms", fmt.Sprintf("%.2f", metrics.AvgResolutionTimeMS),
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

// dnsMetrics mirrors the Metrics struct from the dns canary package.
type dnsMetrics struct {
	RecordType          string              `json:"record_type"`
	AvgResolutionTimeMS float64             `json:"avg_resolution_time_ms"`
	Resolvers           []dnsResolverResult `json:"resolvers"`
}

type dnsResolverResult struct {
	Resolver         string   `json:"resolver"`
	ResolutionTimeMS float64  `json:"resolution_time_ms"`
	ResponseCode     string   `json:"response_code"`
	ResolvedValues   []string `json:"resolved_values"`
	Success          bool     `json:"success"`
	Error            string   `json:"error,omitempty"`
}

// handleHTTPResult processes a single HTTP test result from the transport.
func (p *Processor) handleHTTPResult(_ context.Context, msg []byte) error {
	var result canary.Result
	if err := json.Unmarshal(msg, &result); err != nil {
		p.processingErrors.WithLabelValues("http", "unmarshal").Inc()
		p.logger.Error("failed to unmarshal http result", "error", err)
		return nil
	}

	p.resultsProcessed.WithLabelValues("http", result.POPName).Inc()

	var metrics httpResultMetrics
	if err := json.Unmarshal(result.Metrics, &metrics); err != nil {
		p.processingErrors.WithLabelValues("http", "metrics_unmarshal").Inc()
		p.logger.Error("failed to unmarshal http metrics", "error", err, "test_id", result.TestID)
		return nil
	}

	labels := []string{result.Target, result.POPName, result.AgentID}

	// Phase durations (ms → seconds).
	p.httpDuration.WithLabelValues(append(labels, "dns")...).Set(metrics.DNSMS / 1000.0)
	p.httpDuration.WithLabelValues(append(labels, "tcp")...).Set(metrics.TCPMS / 1000.0)
	p.httpDuration.WithLabelValues(append(labels, "tls")...).Set(metrics.TLSMS / 1000.0)
	p.httpDuration.WithLabelValues(append(labels, "ttfb")...).Set(metrics.TTFBMS / 1000.0)
	p.httpDuration.WithLabelValues(append(labels, "total")...).Set(metrics.TotalMS / 1000.0)
	p.httpDuration.WithLabelValues(append(labels, "transfer")...).Set(metrics.TransferMS / 1000.0)

	p.httpStatusCode.WithLabelValues(labels...).Set(float64(metrics.StatusCode))

	if metrics.CertExpiryDays > 0 {
		p.httpCertExpiry.WithLabelValues(labels...).Set(metrics.CertExpiryDays)
	}

	p.logger.Debug("processed http result",
		"target", result.Target,
		"pop", result.POPName,
		"status", metrics.StatusCode,
		"total_ms", fmt.Sprintf("%.2f", metrics.TotalMS),
	)

	return nil
}

// httpResultMetrics mirrors the Metrics struct from the http canary package.
type httpResultMetrics struct {
	DNSMS          float64 `json:"dns_ms"`
	TCPMS          float64 `json:"tcp_ms"`
	TLSMS          float64 `json:"tls_ms"`
	TTFBMS         float64 `json:"ttfb_ms"`
	TotalMS        float64 `json:"total_ms"`
	TransferMS     float64 `json:"transfer_ms"`
	StatusCode     int     `json:"status_code"`
	ContentLength  int64   `json:"content_length"`
	CertExpiryDays float64 `json:"cert_expiry_days"`
	RedirectCount  int     `json:"redirect_count"`
	ContentMatched bool    `json:"content_matched"`
}

// handleTracerouteResult processes a single traceroute test result from the transport.
func (p *Processor) handleTracerouteResult(_ context.Context, msg []byte) error {
	var result canary.Result
	if err := json.Unmarshal(msg, &result); err != nil {
		p.processingErrors.WithLabelValues("traceroute", "unmarshal").Inc()
		p.logger.Error("failed to unmarshal traceroute result", "error", err)
		return nil
	}

	p.resultsProcessed.WithLabelValues("traceroute", result.POPName).Inc()

	var metrics tracerouteResultMetrics
	if err := json.Unmarshal(result.Metrics, &metrics); err != nil {
		p.processingErrors.WithLabelValues("traceroute", "metrics_unmarshal").Inc()
		p.logger.Error("failed to unmarshal traceroute metrics", "error", err, "test_id", result.TestID)
		return nil
	}

	baseLabels := []string{result.Target, result.POPName, result.AgentID}

	// Hop count.
	p.tracerouteHopCount.WithLabelValues(baseLabels...).Set(float64(metrics.HopCount))

	// Reachable.
	reachable := 0.0
	if metrics.ReachedTarget {
		reachable = 1.0
	}
	p.tracerouteReachable.WithLabelValues(baseLabels...).Set(reachable)

	// Per-hop metrics.
	for _, hop := range metrics.Hops {
		hopNum := strconv.Itoa(hop.HopNumber)
		hopIP := hop.IP
		if hopIP == "" {
			hopIP = "*"
		}
		hopASN := strconv.Itoa(hop.ASN)

		// RTT (ms → seconds).
		rttLabels := append(baseLabels, hopNum, hopIP, hopASN)
		p.tracerouteHopRTT.WithLabelValues(rttLabels...).Set(hop.RTTAvg / 1000.0)

		// Packet loss.
		lossLabels := append(baseLabels, hopNum, hopIP)
		p.tracerouteHopLoss.WithLabelValues(lossLabels...).Set(hop.PacketLoss)
	}

	// Path change detection.
	pathKey := result.Target + "@" + result.POPName
	currentPath := asPathString(metrics.ASPath)
	if lastPath, ok := p.lastASPaths[pathKey]; ok {
		if lastPath != currentPath && currentPath != "" {
			p.traceroutePathChange.WithLabelValues(result.Target, result.POPName).Inc()
			p.logger.Info("AS path change detected",
				"target", result.Target,
				"pop", result.POPName,
				"old_path", lastPath,
				"new_path", currentPath,
			)
		}
	}
	if currentPath != "" {
		p.lastASPaths[pathKey] = currentPath
	}

	p.logger.Debug("processed traceroute result",
		"target", result.Target,
		"pop", result.POPName,
		"hops", metrics.HopCount,
		"reached", metrics.ReachedTarget,
		"as_path", currentPath,
	)

	// Feed into BGP+Traceroute correlation engine (M8).
	p.correlator.HandleTracerouteResult(
		result.Target, result.POPName, result.AgentID,
		metrics.ASPath, metrics.HopCount, metrics.ReachedTarget,
	)

	return nil
}

// asPathString converts an ASN path slice to a string for comparison.
func asPathString(path []int) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, len(path))
	for i, asn := range path {
		parts[i] = strconv.Itoa(asn)
	}
	return strings.Join(parts, " ")
}

// tracerouteResultMetrics mirrors the Metrics struct from the traceroute canary package.
type tracerouteResultMetrics struct {
	Backend       string               `json:"backend"`
	Hops          []tracerouteHopResult `json:"hops"`
	HopCount      int                  `json:"hop_count"`
	ReachedTarget bool                 `json:"reached_target"`
	ASPath        []int                `json:"as_path"`
	TotalMS       float64              `json:"total_ms"`
}

type tracerouteHopResult struct {
	HopNumber  int     `json:"hop_number"`
	IP         string  `json:"ip"`
	Hostname   string  `json:"hostname"`
	ASN        int     `json:"asn"`
	RTTMin     float64 `json:"rtt_min_ms"`
	RTTAvg     float64 `json:"rtt_avg_ms"`
	RTTMax     float64 `json:"rtt_max_ms"`
	PacketLoss float64 `json:"packet_loss_ratio"`
	Sent       int     `json:"sent"`
	Received   int     `json:"received"`
}
