// Package http implements the HTTP/S canary for web service monitoring.
//
// It provides full timing breakdown via httptrace.ClientTrace (DNS, TCP, TLS,
// TTFB, total), status code assertion, content matching, TLS certificate
// validation with expiry tracking, and redirect chain recording.
package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	nethttp "net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
)

// Canary implements the HTTP/S synthetic test.
type Canary struct{}

// New creates a new HTTP canary.
func New() *Canary { return &Canary{} }

// Type returns the canary type identifier.
func (c *Canary) Type() string { return "http" }

// Validate checks whether the HTTP config is valid.
func (c *Canary) Validate(config json.RawMessage) error {
	if len(config) == 0 || string(config) == "{}" || string(config) == "null" {
		return nil
	}

	var cfg Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("invalid http config: %w", err)
	}

	switch strings.ToUpper(cfg.Method) {
	case "", "GET", "POST", "HEAD", "PUT", "DELETE", "PATCH", "OPTIONS":
		// valid
	default:
		return fmt.Errorf("unsupported HTTP method: %s", cfg.Method)
	}

	if cfg.ExpectedStatus < 0 || cfg.ExpectedStatus > 999 {
		return fmt.Errorf("expected_status must be between 0 and 999")
	}

	if cfg.ContentRegex != "" {
		if _, err := regexp.Compile(cfg.ContentRegex); err != nil {
			return fmt.Errorf("invalid content_regex: %w", err)
		}
	}

	if cfg.MaxRedirects < 0 {
		return fmt.Errorf("max_redirects must be non-negative")
	}

	return nil
}

// Execute performs the HTTP request and collects timing metrics.
func (c *Canary) Execute(ctx context.Context, test canary.TestDefinition) (*canary.Result, error) {
	cfg := DefaultConfig()
	if len(test.Config) > 0 && string(test.Config) != "null" {
		if err := json.Unmarshal(test.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse http config: %w", err)
		}
	}
	// Apply defaults for nil pointer fields.
	if cfg.FollowRedirects == nil {
		fr := true
		cfg.FollowRedirects = &fr
	}
	if cfg.ValidateTLS == nil {
		vt := true
		cfg.ValidateTLS = &vt
	}
	if cfg.Method == "" {
		cfg.Method = "GET"
	}
	if cfg.ExpectedStatus == 0 {
		cfg.ExpectedStatus = 200
	}
	if cfg.MaxRedirects == 0 {
		cfg.MaxRedirects = 10
	}

	start := time.Now()
	metrics, err := c.doRequest(ctx, test.Target, cfg)
	totalDuration := time.Since(start)

	result := &canary.Result{
		TestID:     test.ID,
		TestType:   "http",
		Target:     test.Target,
		Timestamp:  start.UTC(),
		DurationMS: float64(totalDuration.Milliseconds()),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		// Still include partial metrics if available.
		if metrics != nil {
			metricsJSON, _ := json.Marshal(metrics)
			result.Metrics = metricsJSON
		}
		return result, nil
	}

	// Check status code.
	statusOK := metrics.StatusCode == cfg.ExpectedStatus

	// Check content match.
	contentOK := metrics.ContentMatched

	result.Success = statusOK && contentOK
	if !statusOK {
		result.Error = fmt.Sprintf("expected status %d, got %d", cfg.ExpectedStatus, metrics.StatusCode)
	} else if !contentOK {
		result.Error = "content match failed"
	}

	metricsJSON, _ := json.Marshal(metrics)
	result.Metrics = metricsJSON

	return result, nil
}

// doRequest performs the actual HTTP request with timing instrumentation.
func (c *Canary) doRequest(ctx context.Context, target string, cfg Config) (*Metrics, error) {
	var trace TimingTrace
	var redirectChain []string

	clientTrace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			trace.DNSStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			trace.DNSDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			trace.ConnectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			trace.ConnectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			trace.TLSStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			trace.TLSDone = time.Now()
		},
		GotFirstResponseByte: func() {
			trace.GotFirstByte = time.Now()
		},
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			trace.RequestDone = time.Now()
		},
	}

	// Build request.
	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = strings.NewReader(cfg.Body)
	}

	req, err := nethttp.NewRequestWithContext(
		httptrace.WithClientTrace(ctx, clientTrace),
		strings.ToUpper(cfg.Method),
		target,
		bodyReader,
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	// Configure client.
	transport := &nethttp.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // user-configurable
		},
	}

	redirectCount := 0
	client := &nethttp.Client{
		Transport: transport,
		CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
			redirectCount++
			redirectChain = append(redirectChain, req.URL.String())
			if !*cfg.FollowRedirects {
				return nethttp.ErrUseLastResponse
			}
			if redirectCount > cfg.MaxRedirects {
				return fmt.Errorf("max redirects (%d) exceeded", cfg.MaxRedirects)
			}
			return nil
		},
	}

	trace.RequestStart = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body for content matching.
	// Limit to 1MB to avoid memory issues.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	requestDone := time.Now()

	// Build metrics.
	metrics := &Metrics{
		StatusCode:    resp.StatusCode,
		ContentLength: resp.ContentLength,
		Protocol:      resp.Proto,
		RedirectCount: redirectCount,
		RedirectChain: redirectChain,
	}

	// Timing breakdown.
	if !trace.DNSStart.IsZero() && !trace.DNSDone.IsZero() {
		metrics.DNSMS = msElapsed(trace.DNSStart, trace.DNSDone)
	}
	if !trace.ConnectStart.IsZero() && !trace.ConnectDone.IsZero() {
		metrics.TCPMS = msElapsed(trace.ConnectStart, trace.ConnectDone)
	}
	if !trace.TLSStart.IsZero() && !trace.TLSDone.IsZero() {
		metrics.TLSMS = msElapsed(trace.TLSStart, trace.TLSDone)
	}
	if !trace.GotFirstByte.IsZero() {
		metrics.TTFBMS = msElapsed(trace.RequestStart, trace.GotFirstByte)
	}
	metrics.TotalMS = msElapsed(trace.RequestStart, requestDone)
	if !trace.GotFirstByte.IsZero() {
		metrics.TransferMS = msElapsed(trace.GotFirstByte, requestDone)
	}

	// TLS info.
	if resp.TLS != nil {
		metrics.TLSVersion = tlsVersionString(resp.TLS.Version)
		metrics.TLSCipher = tls.CipherSuiteName(resp.TLS.CipherSuite)

		if len(resp.TLS.PeerCertificates) > 0 {
			cert := resp.TLS.PeerCertificates[0]
			metrics.CertSubject = cert.Subject.CommonName
			metrics.CertIssuer = cert.Issuer.CommonName
			metrics.CertExpiryDays = math.Floor(time.Until(cert.NotAfter).Hours()/24*10) / 10
		}
	}

	// Content validation.
	metrics.ContentMatched = true
	if cfg.ContentMatch != "" {
		metrics.ContentMatched = strings.Contains(string(body), cfg.ContentMatch)
	}
	if cfg.ContentRegex != "" && metrics.ContentMatched {
		re, err := regexp.Compile(cfg.ContentRegex)
		if err == nil {
			metrics.ContentMatched = re.Match(body)
		} else {
			metrics.ContentMatched = false
		}
	}

	return metrics, nil
}

func msElapsed(start, end time.Time) float64 {
	return float64(end.Sub(start).Microseconds()) / 1000.0
}

func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}
