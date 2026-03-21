// Package correlation implements the BGP + Traceroute path correlation engine.
//
// This is what ties both systems together: it compares the AS path BGP announces
// against the AS path traceroute actually observes. Discrepancies reveal route
// leaks, traffic engineering problems, and hijacks that neither system catches
// alone. This is ThousandEyes-grade capability.
//
// Architecture:
//
//	BGP Analyzer (Python) → publishes BGP AS paths to NATS topic
//	Traceroute Canary (Go) → publishes traceroute results with AS paths
//	Correlation Engine (this) → consumes both, compares, alerts on mismatches
//
// The engine maintains an in-memory store of the latest BGP-observed AS path
// and the latest traceroute-observed AS path for each (prefix, POP) pair.
// When either side updates, it re-evaluates the correlation.
package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Engine performs BGP + traceroute AS path correlation.
type Engine struct {
	mu     sync.RWMutex
	logger *slog.Logger

	// bgpPaths stores the latest BGP-announced AS path per prefix.
	// Key: prefix (e.g., "8.8.8.0/24") → BGPPathInfo
	bgpPaths map[string]*BGPPathInfo

	// traceroutePaths stores the latest traceroute-observed AS path per (target, pop).
	// Key: "target@pop" → TraceroutePathInfo
	traceroutePaths map[string]*TraceroutePathInfo

	// prefixTargetMap maps IP targets to their covering prefixes for correlation.
	// Key: target → prefix. Populated as BGP updates arrive.
	prefixTargetMap map[string]string

	// Prometheus metrics.
	mismatchTotal   *prometheus.CounterVec
	correlationAge  *prometheus.GaugeVec
	matchStatus     *prometheus.GaugeVec
	correlationsRun *prometheus.CounterVec
}

// BGPPathInfo holds the latest BGP-announced path for a prefix.
type BGPPathInfo struct {
	Prefix    string    `json:"prefix"`
	OriginASN int       `json:"origin_asn"`
	ASPath    []int     `json:"as_path"`
	PeerASN   int       `json:"peer_asn"`
	Collector string    `json:"collector"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TraceroutePathInfo holds the latest traceroute-observed path for a target.
type TraceroutePathInfo struct {
	Target    string    `json:"target"`
	POPName   string    `json:"pop_name"`
	AgentID   string    `json:"agent_id"`
	ASPath    []int     `json:"as_path"`
	HopCount  int       `json:"hop_count"`
	Reached   bool      `json:"reached_target"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CorrelationResult is the output of comparing a BGP path with a traceroute path.
type CorrelationResult struct {
	Prefix         string    `json:"prefix"`
	Target         string    `json:"target"`
	POPName        string    `json:"pop_name"`
	BGPPath        []int     `json:"bgp_as_path"`
	TraceroutePath []int     `json:"traceroute_as_path"`
	Match          MatchType `json:"match"`
	Details        string    `json:"details"`
	Timestamp      time.Time `json:"timestamp"`
}

// MatchType classifies the correlation result.
type MatchType string

const (
	// MatchExact means the traceroute AS path is a contiguous subsequence
	// of the BGP AS path (normal — traceroute sees a tail of the full path).
	MatchExact MatchType = "exact"

	// MatchPartial means the paths share the same origin AS but diverge
	// in transit (possible traffic engineering or partial visibility).
	MatchPartial MatchType = "partial"

	// MatchMismatch means the origin AS differs or paths have no overlap
	// (potential hijack, route leak, or misconfiguration).
	MatchMismatch MatchType = "mismatch"

	// MatchInsufficient means we don't have enough data to correlate
	// (e.g., traceroute didn't reach target or has no ASN data).
	MatchInsufficient MatchType = "insufficient"
)

// New creates a new correlation engine with registered Prometheus metrics.
func New(reg prometheus.Registerer, logger *slog.Logger) *Engine {
	e := &Engine{
		logger:          logger,
		bgpPaths:        make(map[string]*BGPPathInfo),
		traceroutePaths: make(map[string]*TraceroutePathInfo),
		prefixTargetMap: make(map[string]string),

		mismatchTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netvantage_path_correlation_mismatch_total",
			Help: "Total AS path mismatches between BGP and traceroute",
		}, []string{"prefix", "pop", "match_type"}),

		correlationAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_path_correlation_age_seconds",
			Help: "Seconds since last correlation was evaluated for a prefix/pop pair",
		}, []string{"prefix", "pop"}),

		matchStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netvantage_path_correlation_status",
			Help: "Current correlation status (1=exact, 0.5=partial, 0=mismatch, -1=insufficient)",
		}, []string{"prefix", "pop", "target"}),

		correlationsRun: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netvantage_path_correlation_evaluations_total",
			Help: "Total correlation evaluations performed",
		}, []string{"prefix", "pop"}),
	}

	reg.MustRegister(e.mismatchTotal)
	reg.MustRegister(e.correlationAge)
	reg.MustRegister(e.matchStatus)
	reg.MustRegister(e.correlationsRun)

	return e
}

// BGPUpdate is the message format published by the BGP Analyzer to NATS
// for correlation purposes. The BGP Analyzer publishes these to
// netvantage.bgp.paths when it detects an announcement for a monitored prefix.
type BGPUpdate struct {
	Prefix    string `json:"prefix"`
	OriginASN int    `json:"origin_asn"`
	ASPath    []int  `json:"as_path"`
	PeerASN   int    `json:"peer_asn"`
	Collector string `json:"collector"`
	EventType string `json:"event_type"` // "announcement" or "withdrawal"
	Timestamp string `json:"timestamp"`
}

// HandleBGPUpdate processes a BGP path update from the transport.
func (e *Engine) HandleBGPUpdate(_ context.Context, msg []byte) error {
	var update BGPUpdate
	if err := json.Unmarshal(msg, &update); err != nil {
		e.logger.Error("failed to unmarshal BGP update", "error", err)
		return nil
	}

	if update.EventType == "withdrawal" {
		e.mu.Lock()
		delete(e.bgpPaths, update.Prefix)
		e.mu.Unlock()
		e.logger.Debug("bgp path withdrawn", "prefix", update.Prefix)
		return nil
	}

	info := &BGPPathInfo{
		Prefix:    update.Prefix,
		OriginASN: update.OriginASN,
		ASPath:    update.ASPath,
		PeerASN:   update.PeerASN,
		Collector: update.Collector,
		UpdatedAt: time.Now(),
	}

	e.mu.Lock()
	e.bgpPaths[update.Prefix] = info
	e.mu.Unlock()

	e.logger.Debug("bgp path updated",
		"prefix", update.Prefix,
		"origin", update.OriginASN,
		"as_path", asPathStr(update.ASPath),
	)

	// Trigger correlation for any traceroute targets covered by this prefix.
	e.correlatePrefix(update.Prefix)

	return nil
}

// TracerouteUpdate is the relevant subset of traceroute results for correlation.
type TracerouteUpdate struct {
	Target    string `json:"target"`
	POPName   string `json:"pop_name"`
	AgentID   string `json:"agent_id"`
	ASPath    []int  `json:"as_path"`
	HopCount  int    `json:"hop_count"`
	Reached   bool   `json:"reached_target"`
}

// HandleTracerouteResult processes a traceroute result for correlation.
// Called by the processor after it handles the traceroute metrics.
func (e *Engine) HandleTracerouteResult(target, popName, agentID string, asPath []int, hopCount int, reached bool) {
	info := &TraceroutePathInfo{
		Target:    target,
		POPName:   popName,
		AgentID:   agentID,
		ASPath:    asPath,
		HopCount:  hopCount,
		Reached:   reached,
		UpdatedAt: time.Now(),
	}

	key := target + "@" + popName
	e.mu.Lock()
	e.traceroutePaths[key] = info
	e.mu.Unlock()

	e.logger.Debug("traceroute path updated",
		"target", target,
		"pop", popName,
		"as_path", asPathStr(asPath),
		"reached", reached,
	)

	// Try to correlate against known BGP paths.
	e.correlateTarget(target, popName)
}

// RegisterPrefix maps a target IP/host to a BGP prefix for correlation.
// Called when test definitions are loaded so the engine knows which targets
// correspond to which BGP-monitored prefixes.
func (e *Engine) RegisterPrefix(target, prefix string) {
	e.mu.Lock()
	e.prefixTargetMap[target] = prefix
	e.mu.Unlock()
	e.logger.Debug("registered target-prefix mapping", "target", target, "prefix", prefix)
}

// correlateTarget evaluates the correlation for a specific target/POP pair.
func (e *Engine) correlateTarget(target, popName string) {
	e.mu.RLock()
	prefix, ok := e.prefixTargetMap[target]
	if !ok {
		e.mu.RUnlock()
		return // No prefix mapping for this target — can't correlate.
	}

	bgpInfo := e.bgpPaths[prefix]
	trKey := target + "@" + popName
	trInfo := e.traceroutePaths[trKey]
	e.mu.RUnlock()

	if bgpInfo == nil || trInfo == nil {
		return // Need both sides for correlation.
	}

	result := e.evaluate(bgpInfo, trInfo)
	e.recordResult(result)
}

// correlatePrefix evaluates all targets that map to a given prefix.
func (e *Engine) correlatePrefix(prefix string) {
	e.mu.RLock()
	var targets []string
	for target, p := range e.prefixTargetMap {
		if p == prefix {
			targets = append(targets, target)
		}
	}
	e.mu.RUnlock()

	for _, target := range targets {
		e.mu.RLock()
		for key, trInfo := range e.traceroutePaths {
			if trInfo.Target == target {
				pop := strings.TrimPrefix(key, target+"@")
				e.mu.RUnlock()
				e.correlateTarget(target, pop)
				e.mu.RLock()
			}
		}
		e.mu.RUnlock()
	}
}

// evaluate compares a BGP path against a traceroute path and classifies the result.
func (e *Engine) evaluate(bgp *BGPPathInfo, tr *TraceroutePathInfo) *CorrelationResult {
	result := &CorrelationResult{
		Prefix:         bgp.Prefix,
		Target:         tr.Target,
		POPName:        tr.POPName,
		BGPPath:        bgp.ASPath,
		TraceroutePath: tr.ASPath,
		Timestamp:      time.Now(),
	}

	// Not enough traceroute data to correlate.
	if !tr.Reached || len(tr.ASPath) < 2 {
		result.Match = MatchInsufficient
		result.Details = "traceroute did not reach target or has insufficient AS data"
		return result
	}

	if len(bgp.ASPath) == 0 {
		result.Match = MatchInsufficient
		result.Details = "BGP path is empty"
		return result
	}

	bgpOrigin := bgp.ASPath[len(bgp.ASPath)-1]
	trOrigin := tr.ASPath[len(tr.ASPath)-1]

	// Check origin AS match first — most important signal.
	if bgpOrigin != trOrigin {
		result.Match = MatchMismatch
		result.Details = fmt.Sprintf(
			"origin AS mismatch: BGP says AS%d, traceroute observed AS%d",
			bgpOrigin, trOrigin,
		)
		return result
	}

	// Origin matches. Check if traceroute path is a subsequence of BGP path.
	if isSubsequence(tr.ASPath, bgp.ASPath) {
		result.Match = MatchExact
		result.Details = "traceroute AS path is consistent with BGP-announced path"
		return result
	}

	// Origin matches but paths diverge in transit.
	overlap := pathOverlap(bgp.ASPath, tr.ASPath)
	if overlap > 0 {
		result.Match = MatchPartial
		result.Details = fmt.Sprintf(
			"origin AS matches (AS%d) but transit paths diverge; %d ASNs overlap",
			bgpOrigin, overlap,
		)
		return result
	}

	// No overlap at all besides origin.
	result.Match = MatchMismatch
	result.Details = fmt.Sprintf(
		"origin AS matches (AS%d) but no transit path overlap",
		bgpOrigin,
	)
	return result
}

// recordResult publishes correlation metrics and logs notable findings.
func (e *Engine) recordResult(result *CorrelationResult) {
	e.correlationsRun.WithLabelValues(result.Prefix, result.POPName).Inc()
	e.correlationAge.WithLabelValues(result.Prefix, result.POPName).Set(0)

	var statusVal float64
	switch result.Match {
	case MatchExact:
		statusVal = 1.0
	case MatchPartial:
		statusVal = 0.5
		e.mismatchTotal.WithLabelValues(result.Prefix, result.POPName, "partial").Inc()
	case MatchMismatch:
		statusVal = 0.0
		e.mismatchTotal.WithLabelValues(result.Prefix, result.POPName, "mismatch").Inc()
		e.logger.Warn("path correlation mismatch detected",
			"prefix", result.Prefix,
			"target", result.Target,
			"pop", result.POPName,
			"bgp_path", asPathStr(result.BGPPath),
			"traceroute_path", asPathStr(result.TraceroutePath),
			"details", result.Details,
		)
	case MatchInsufficient:
		statusVal = -1.0
	}

	e.matchStatus.WithLabelValues(result.Prefix, result.POPName, result.Target).Set(statusVal)
}

// GetCorrelationState returns the current correlation state for debugging/API use.
func (e *Engine) GetCorrelationState() map[string]*CorrelationResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	results := make(map[string]*CorrelationResult)
	for key, trInfo := range e.traceroutePaths {
		prefix, ok := e.prefixTargetMap[trInfo.Target]
		if !ok {
			continue
		}
		bgpInfo, ok := e.bgpPaths[prefix]
		if !ok {
			continue
		}
		results[key] = e.evaluate(bgpInfo, trInfo)
	}
	return results
}

// isSubsequence checks if `sub` is a contiguous subsequence of `full`.
// Traceroute typically sees the tail of the BGP AS path (from some transit
// point through to the origin), so we check if the traceroute path appears
// as a contiguous segment within the BGP path.
func isSubsequence(sub, full []int) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(full) {
		return false
	}

	for i := 0; i <= len(full)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if full[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// pathOverlap counts how many ASNs appear in both paths (set intersection).
func pathOverlap(a, b []int) int {
	set := make(map[int]bool, len(a))
	for _, asn := range a {
		set[asn] = true
	}
	count := 0
	for _, asn := range b {
		if set[asn] {
			count++
		}
	}
	return count
}

// asPathStr formats an AS path slice as a readable string.
func asPathStr(path []int) string {
	parts := make([]string, len(path))
	for i, asn := range path {
		parts[i] = strconv.Itoa(asn)
	}
	return strings.Join(parts, " → ")
}
