package correlation

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	return New(reg, logger)
}

// --- isSubsequence tests ---

func TestIsSubsequence(t *testing.T) {
	cases := []struct {
		name string
		sub  []int
		full []int
		want bool
	}{
		{"exact match", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"tail match", []int{2, 3}, []int{1, 2, 3}, true},
		{"head match", []int{1, 2}, []int{1, 2, 3}, true},
		{"middle match", []int{2, 3}, []int{1, 2, 3, 4}, true},
		{"no match", []int{1, 3}, []int{1, 2, 3}, false},
		{"sub longer than full", []int{1, 2, 3, 4}, []int{1, 2, 3}, false},
		{"empty sub", []int{}, []int{1, 2, 3}, true},
		{"single element match", []int{2}, []int{1, 2, 3}, true},
		{"single element no match", []int{5}, []int{1, 2, 3}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSubsequence(tc.sub, tc.full)
			if got != tc.want {
				t.Fatalf("isSubsequence(%v, %v) = %v, want %v", tc.sub, tc.full, got, tc.want)
			}
		})
	}
}

// --- pathOverlap tests ---

func TestPathOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b []int
		want int
	}{
		{"full overlap", []int{1, 2, 3}, []int{1, 2, 3}, 3},
		{"partial overlap", []int{1, 2, 3}, []int{2, 3, 4}, 2},
		{"no overlap", []int{1, 2}, []int{3, 4}, 0},
		{"empty paths", []int{}, []int{1, 2}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathOverlap(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("pathOverlap(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// --- evaluate tests ---

func TestEvaluate_ExactMatch(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{
		Prefix:    "8.8.8.0/24",
		OriginASN: 15169,
		ASPath:    []int{64496, 3356, 15169},
	}
	tr := &TraceroutePathInfo{
		Target:  "8.8.8.8",
		POPName: "us-east-1",
		ASPath:  []int{3356, 15169}, // Tail of BGP path — expected.
		Reached: true,
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchExact {
		t.Fatalf("expected MatchExact, got %s: %s", result.Match, result.Details)
	}
}

func TestEvaluate_PartialMatch(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{
		Prefix:    "1.1.1.0/24",
		OriginASN: 13335,
		ASPath:    []int{64496, 3356, 13335},
	}
	tr := &TraceroutePathInfo{
		Target:  "1.1.1.1",
		POPName: "eu-west-1",
		ASPath:  []int{174, 13335}, // Different transit, same origin.
		Reached: true,
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchPartial {
		t.Fatalf("expected MatchPartial, got %s: %s", result.Match, result.Details)
	}
}

func TestEvaluate_OriginMismatch(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{
		Prefix:    "8.8.8.0/24",
		OriginASN: 15169,
		ASPath:    []int{64496, 3356, 15169},
	}
	tr := &TraceroutePathInfo{
		Target:  "8.8.8.8",
		POPName: "us-east-1",
		ASPath:  []int{3356, 99999}, // Different origin — potential hijack.
		Reached: true,
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchMismatch {
		t.Fatalf("expected MatchMismatch, got %s: %s", result.Match, result.Details)
	}
}

func TestEvaluate_InsufficientData_NotReached(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{
		Prefix: "8.8.8.0/24",
		ASPath: []int{64496, 15169},
	}
	tr := &TraceroutePathInfo{
		Target:  "8.8.8.8",
		POPName: "us-east-1",
		ASPath:  []int{64496},
		Reached: false, // Didn't reach target.
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchInsufficient {
		t.Fatalf("expected MatchInsufficient, got %s", result.Match)
	}
}

func TestEvaluate_InsufficientData_ShortPath(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{
		Prefix: "8.8.8.0/24",
		ASPath: []int{64496, 15169},
	}
	tr := &TraceroutePathInfo{
		Target:  "8.8.8.8",
		POPName: "us-east-1",
		ASPath:  []int{15169}, // Only 1 ASN — not enough to compare.
		Reached: true,
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchInsufficient {
		t.Fatalf("expected MatchInsufficient, got %s", result.Match)
	}
}

func TestEvaluate_EmptyBGPPath(t *testing.T) {
	e := testEngine(t)

	bgp := &BGPPathInfo{Prefix: "8.8.8.0/24", ASPath: []int{}}
	tr := &TraceroutePathInfo{
		Target: "8.8.8.8", POPName: "us-east-1",
		ASPath: []int{3356, 15169}, Reached: true,
	}

	result := e.evaluate(bgp, tr)
	if result.Match != MatchInsufficient {
		t.Fatalf("expected MatchInsufficient, got %s", result.Match)
	}
}

// --- HandleBGPUpdate tests ---

func TestHandleBGPUpdate_Announcement(t *testing.T) {
	e := testEngine(t)

	update := BGPUpdate{
		Prefix:    "8.8.8.0/24",
		OriginASN: 15169,
		ASPath:    []int{64496, 3356, 15169},
		PeerASN:   64496,
		Collector: "route-views2",
		EventType: "announcement",
	}
	data, _ := json.Marshal(update)

	if err := e.HandleBGPUpdate(context.Background(), data); err != nil {
		t.Fatal(err)
	}

	e.mu.RLock()
	info, ok := e.bgpPaths["8.8.8.0/24"]
	e.mu.RUnlock()

	if !ok {
		t.Fatal("expected BGP path to be stored")
	}
	if info.OriginASN != 15169 {
		t.Fatalf("expected origin 15169, got %d", info.OriginASN)
	}
	if len(info.ASPath) != 3 {
		t.Fatalf("expected 3 ASNs, got %d", len(info.ASPath))
	}
}

func TestHandleBGPUpdate_Withdrawal(t *testing.T) {
	e := testEngine(t)

	// First add a path.
	ann := BGPUpdate{
		Prefix: "8.8.8.0/24", OriginASN: 15169, ASPath: []int{64496, 15169},
		EventType: "announcement",
	}
	data, _ := json.Marshal(ann)
	_ = e.HandleBGPUpdate(context.Background(), data)

	// Then withdraw it.
	wd := BGPUpdate{Prefix: "8.8.8.0/24", EventType: "withdrawal"}
	data, _ = json.Marshal(wd)
	_ = e.HandleBGPUpdate(context.Background(), data)

	e.mu.RLock()
	_, ok := e.bgpPaths["8.8.8.0/24"]
	e.mu.RUnlock()

	if ok {
		t.Fatal("expected BGP path to be removed after withdrawal")
	}
}

func TestHandleBGPUpdate_InvalidJSON(t *testing.T) {
	e := testEngine(t)
	err := e.HandleBGPUpdate(context.Background(), []byte("not json"))
	if err != nil {
		t.Fatal("expected nil error for invalid JSON (logged, not propagated)")
	}
}

// --- HandleTracerouteResult tests ---

func TestHandleTracerouteResult(t *testing.T) {
	e := testEngine(t)

	e.HandleTracerouteResult("8.8.8.8", "us-east-1", "agent-01", []int{3356, 15169}, 5, true)

	e.mu.RLock()
	info, ok := e.traceroutePaths["8.8.8.8@us-east-1"]
	e.mu.RUnlock()

	if !ok {
		t.Fatal("expected traceroute path to be stored")
	}
	if len(info.ASPath) != 2 {
		t.Fatalf("expected 2 ASNs, got %d", len(info.ASPath))
	}
	if info.POPName != "us-east-1" {
		t.Fatalf("expected pop us-east-1, got %s", info.POPName)
	}
}

// --- End-to-end correlation test ---

func TestEndToEndCorrelation_Mismatch(t *testing.T) {
	e := testEngine(t)

	// Register target→prefix mapping.
	e.RegisterPrefix("8.8.8.8", "8.8.8.0/24")

	// BGP says origin is AS15169.
	bgpData, _ := json.Marshal(BGPUpdate{
		Prefix: "8.8.8.0/24", OriginASN: 15169, ASPath: []int{64496, 3356, 15169},
		EventType: "announcement",
	})
	_ = e.HandleBGPUpdate(context.Background(), bgpData)

	// Traceroute sees a DIFFERENT origin — AS99999 (hijack scenario).
	e.HandleTracerouteResult("8.8.8.8", "us-east-1", "agent-01", []int{3356, 99999}, 5, true)

	// Check correlation state.
	state := e.GetCorrelationState()
	result, ok := state["8.8.8.8@us-east-1"]
	if !ok {
		t.Fatal("expected correlation result")
	}
	if result.Match != MatchMismatch {
		t.Fatalf("expected MatchMismatch, got %s: %s", result.Match, result.Details)
	}
}

func TestEndToEndCorrelation_ExactMatch(t *testing.T) {
	e := testEngine(t)

	e.RegisterPrefix("1.1.1.1", "1.1.1.0/24")

	bgpData, _ := json.Marshal(BGPUpdate{
		Prefix: "1.1.1.0/24", OriginASN: 13335, ASPath: []int{64496, 174, 13335},
		EventType: "announcement",
	})
	_ = e.HandleBGPUpdate(context.Background(), bgpData)

	// Traceroute sees tail of BGP path — normal case.
	e.HandleTracerouteResult("1.1.1.1", "eu-west-1", "agent-02", []int{174, 13335}, 8, true)

	state := e.GetCorrelationState()
	result, ok := state["1.1.1.1@eu-west-1"]
	if !ok {
		t.Fatal("expected correlation result")
	}
	if result.Match != MatchExact {
		t.Fatalf("expected MatchExact, got %s: %s", result.Match, result.Details)
	}
}

func TestRegisterPrefix(t *testing.T) {
	e := testEngine(t)
	e.RegisterPrefix("8.8.8.8", "8.8.8.0/24")

	e.mu.RLock()
	prefix, ok := e.prefixTargetMap["8.8.8.8"]
	e.mu.RUnlock()

	if !ok || prefix != "8.8.8.0/24" {
		t.Fatalf("expected 8.8.8.0/24, got %s", prefix)
	}
}

func TestAsPathStr(t *testing.T) {
	got := asPathStr([]int{64496, 3356, 15169})
	if got != "64496 → 3356 → 15169" {
		t.Fatalf("unexpected: %s", got)
	}
}
