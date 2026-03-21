// Package dns implements the DNS resolution canary for NetVantage.
//
// The DNS canary queries specified resolvers for DNS records and measures
// resolution time, validates response codes, and optionally asserts that
// resolved values match expected content. It supports all common record types
// (A, AAAA, CNAME, MX, NS, TXT, SOA, SRV) and can compare results across
// multiple resolvers to detect resolver-level issues.
//
// Unlike ping (which tests raw reachability), the DNS canary tests the
// resolution layer — the first thing that breaks when DNS infrastructure
// has problems, and often the hardest to debug after the fact.
package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
)

// Canary implements the canary.Canary interface for DNS resolution tests.
type Canary struct{}

// New creates a new DNS canary instance.
func New() *Canary {
	return &Canary{}
}

// Type returns "dns".
func (c *Canary) Type() string {
	return "dns"
}

// Validate checks that DNS-specific configuration is valid.
func (c *Canary) Validate(config json.RawMessage) error {
	if len(config) == 0 {
		return nil
	}
	var cfg Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("dns: invalid config: %w", err)
	}
	if cfg.RecordType != "" {
		valid := map[string]bool{
			"A": true, "AAAA": true, "CNAME": true, "MX": true,
			"NS": true, "TXT": true, "SOA": true, "SRV": true,
		}
		if !valid[strings.ToUpper(cfg.RecordType)] {
			return fmt.Errorf("dns: unsupported record type %q", cfg.RecordType)
		}
	}
	return nil
}

// Execute runs a DNS resolution test against the target (domain name)
// using the configured resolver(s) and record type.
func (c *Canary) Execute(ctx context.Context, test canary.TestDefinition) (*canary.Result, error) {
	cfg := DefaultConfig()
	if len(test.Config) > 0 {
		if err := json.Unmarshal(test.Config, &cfg); err != nil {
			return nil, fmt.Errorf("dns: parse config: %w", err)
		}
	}

	resolvers := cfg.Resolvers
	if len(resolvers) == 0 {
		resolvers = []string{""} // Empty string = system default resolver.
	}

	start := time.Now()
	var allResults []ResolverResult

	for _, resolver := range resolvers {
		rr := c.queryResolver(ctx, test.Target, resolver, cfg)
		allResults = append(allResults, rr)
	}

	elapsed := time.Since(start)

	// Determine overall success: all resolvers must succeed and pass validation.
	success := true
	for _, rr := range allResults {
		if !rr.Success {
			success = false
			break
		}
	}

	metrics := Metrics{
		RecordType: strings.ToUpper(cfg.RecordType),
		Resolvers:  allResults,
	}

	// Compute aggregate resolution time (average across resolvers).
	var totalTime float64
	for _, rr := range allResults {
		totalTime += rr.ResolutionTimeMS
	}
	if len(allResults) > 0 {
		metrics.AvgResolutionTimeMS = totalTime / float64(len(allResults))
	}

	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("dns: marshal metrics: %w", err)
	}

	// Build error message from any failed resolvers.
	var errMsg string
	for _, rr := range allResults {
		if rr.Error != "" {
			if errMsg != "" {
				errMsg += "; "
			}
			errMsg += fmt.Sprintf("%s: %s", rr.Resolver, rr.Error)
		}
	}

	return &canary.Result{
		TestID:     test.ID,
		TestType:   "dns",
		Target:     test.Target,
		Timestamp:  start.UTC(),
		DurationMS: float64(elapsed.Milliseconds()),
		Success:    success,
		Metrics:    metricsJSON,
		Error:      errMsg,
	}, nil
}

// queryResolver performs a DNS query against a single resolver.
func (c *Canary) queryResolver(ctx context.Context, domain, resolver string, cfg Config) ResolverResult {
	rr := ResolverResult{
		Resolver: resolver,
		Success:  true,
	}
	if resolver == "" {
		rr.Resolver = "system"
	}

	// Build a custom resolver if a specific server is configured.
	var r *net.Resolver
	if resolver != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				addr := resolver
				if !strings.Contains(addr, ":") {
					addr = addr + ":53"
				}
				return d.DialContext(ctx, "udp", addr)
			},
		}
	} else {
		r = net.DefaultResolver
	}

	start := time.Now()
	recordType := strings.ToUpper(cfg.RecordType)
	if recordType == "" {
		recordType = "A"
	}

	var resolved []string
	var responseCode string
	var queryErr error

	switch recordType {
	case "A", "AAAA":
		ips, err := r.LookupIPAddr(ctx, domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			for _, ip := range ips {
				if recordType == "A" && ip.IP.To4() != nil {
					resolved = append(resolved, ip.IP.String())
				} else if recordType == "AAAA" && ip.IP.To4() == nil {
					resolved = append(resolved, ip.IP.String())
				}
			}
		}
	case "CNAME":
		cname, err := r.LookupCNAME(ctx, domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			resolved = append(resolved, cname)
		}
	case "MX":
		mxs, err := r.LookupMX(ctx, domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			for _, mx := range mxs {
				resolved = append(resolved, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
			}
		}
	case "NS":
		nss, err := r.LookupNS(ctx, domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			for _, ns := range nss {
				resolved = append(resolved, ns.Host)
			}
		}
	case "TXT":
		txts, err := r.LookupTXT(ctx, domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			resolved = txts
		}
	case "SRV":
		_, addrs, err := r.LookupSRV(ctx, "", "", domain)
		queryErr = err
		if err == nil {
			responseCode = "NOERROR"
			for _, addr := range addrs {
				resolved = append(resolved, fmt.Sprintf("%d %d %d %s",
					addr.Priority, addr.Weight, addr.Port, addr.Target))
			}
		}
	default:
		queryErr = fmt.Errorf("unsupported record type: %s", recordType)
	}

	rr.ResolutionTimeMS = float64(time.Since(start).Microseconds()) / 1000.0
	rr.ResolvedValues = resolved

	if queryErr != nil {
		rr.Success = false
		rr.Error = queryErr.Error()
		responseCode = classifyDNSError(queryErr)
	}
	rr.ResponseCode = responseCode

	// Content validation: assert expected values if configured.
	if rr.Success && len(cfg.ExpectedValues) > 0 {
		if !validateContent(resolved, cfg.ExpectedValues) {
			rr.Success = false
			rr.Error = fmt.Sprintf("content validation failed: expected %v, got %v",
				cfg.ExpectedValues, resolved)
		}
	}

	return rr
}

// classifyDNSError maps Go DNS errors to standard DNS response codes.
func classifyDNSError(err error) string {
	if err == nil {
		return "NOERROR"
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "no such host"):
		return "NXDOMAIN"
	case strings.Contains(errStr, "server misbehaving"):
		return "SERVFAIL"
	case strings.Contains(errStr, "i/o timeout"):
		return "TIMEOUT"
	case strings.Contains(errStr, "connection refused"):
		return "REFUSED"
	default:
		return "ERROR"
	}
}

// validateContent checks that all expected values appear in the resolved set.
func validateContent(resolved, expected []string) bool {
	resolvedSet := make(map[string]bool, len(resolved))
	for _, v := range resolved {
		resolvedSet[v] = true
	}
	for _, exp := range expected {
		if !resolvedSet[exp] {
			return false
		}
	}
	return true
}
