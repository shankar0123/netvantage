package traceroute

import (
	"encoding/json"
	"testing"
)

// --- Config tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backend != "mtr" {
		t.Fatalf("expected mtr, got %s", cfg.Backend)
	}
	if cfg.Cycles != 10 {
		t.Fatalf("expected 10 cycles, got %d", cfg.Cycles)
	}
	if cfg.MaxHops != 30 {
		t.Fatalf("expected 30 max_hops, got %d", cfg.MaxHops)
	}
	if cfg.Protocol != "icmp" {
		t.Fatalf("expected icmp, got %s", cfg.Protocol)
	}
	if cfg.ResolveASN == nil || !*cfg.ResolveASN {
		t.Fatal("expected resolve_asn to be true")
	}
	if cfg.ResolveHostnames == nil || !*cfg.ResolveHostnames {
		t.Fatal("expected resolve_hostnames to be true")
	}
}

func TestConfigUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		json string
		want func(Config) bool
	}{
		{
			"scamper backend",
			`{"backend":"scamper","cycles":5}`,
			func(c Config) bool { return c.Backend == "scamper" && c.Cycles == 5 },
		},
		{
			"tcp protocol with port",
			`{"protocol":"tcp","port":443}`,
			func(c Config) bool { return c.Protocol == "tcp" && c.Port == 443 },
		},
		{
			"disable ASN resolution",
			`{"resolve_asn":false}`,
			func(c Config) bool { return c.ResolveASN != nil && !*c.ResolveASN },
		},
		{
			"custom max_hops and packet_size",
			`{"max_hops":64,"packet_size":128}`,
			func(c Config) bool { return c.MaxHops == 64 && c.PacketSize == 128 },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal([]byte(tc.json), &cfg); err != nil {
				t.Fatal(err)
			}
			if !tc.want(cfg) {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}

// --- Validate tests ---

func TestValidate(t *testing.T) {
	c := New()

	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"empty config is valid", "{}", false},
		{"null config is valid", "null", false},
		{"mtr backend is valid", `{"backend":"mtr"}`, false},
		{"scamper backend is valid", `{"backend":"scamper"}`, false},
		{"invalid backend", `{"backend":"ping"}`, true},
		{"negative cycles", `{"cycles":-1}`, true},
		{"negative max_hops", `{"max_hops":-1}`, true},
		{"max_hops too high", `{"max_hops":256}`, true},
		{"valid protocol icmp", `{"protocol":"icmp"}`, false},
		{"valid protocol tcp", `{"protocol":"tcp"}`, false},
		{"valid protocol udp", `{"protocol":"udp"}`, false},
		{"invalid protocol", `{"protocol":"quic"}`, true},
		{"negative interval", `{"interval_sec":-0.5}`, true},
		{"invalid json", `not json`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.Validate(json.RawMessage(tc.config))
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- mtr JSON parsing tests ---

func TestParseMTRJSON(t *testing.T) {
	// Realistic mtr --json output.
	mtrOutput := `{
		"report": {
			"mtr": {
				"src": "agent-01",
				"dst": "8.8.8.8",
				"tos": 0,
				"tests": 10,
				"psize": "64",
				"bitpattern": "0x00"
			},
			"hubs": [
				{"count": 1, "host": "192.168.1.1", "Loss%": 0.0, "Snt": 10, "Last": 1.2, "Avg": 1.5, "Best": 0.8, "Wrst": 3.1, "StDev": 0.5},
				{"count": 2, "host": "10.0.0.1", "Loss%": 10.0, "Snt": 10, "Last": 5.0, "Avg": 4.8, "Best": 3.2, "Wrst": 7.1, "StDev": 1.2},
				{"count": 3, "host": "???", "Loss%": 100.0, "Snt": 10, "Last": 0.0, "Avg": 0.0, "Best": 0.0, "Wrst": 0.0, "StDev": 0.0},
				{"count": 4, "host": "72.14.236.216", "Loss%": 0.0, "Snt": 10, "Last": 10.5, "Avg": 11.2, "Best": 9.8, "Wrst": 15.3, "StDev": 1.8},
				{"count": 5, "host": "8.8.8.8", "Loss%": 0.0, "Snt": 10, "Last": 12.1, "Avg": 12.5, "Best": 11.0, "Wrst": 16.0, "StDev": 1.5}
			]
		}
	}`

	hops, err := parseMTRJSON([]byte(mtrOutput))
	if err != nil {
		t.Fatal(err)
	}

	if len(hops) != 5 {
		t.Fatalf("expected 5 hops, got %d", len(hops))
	}

	// First hop.
	if hops[0].HopNumber != 1 {
		t.Fatalf("expected hop 1, got %d", hops[0].HopNumber)
	}
	if hops[0].IP != "192.168.1.1" {
		t.Fatalf("expected IP 192.168.1.1, got %s", hops[0].IP)
	}
	if hops[0].RTTAvg != 1.5 {
		t.Fatalf("expected avg RTT 1.5, got %f", hops[0].RTTAvg)
	}
	if hops[0].PacketLoss != 0.0 {
		t.Fatalf("expected 0 loss, got %f", hops[0].PacketLoss)
	}

	// Second hop — 10% loss.
	if hops[1].PacketLoss != 0.1 {
		t.Fatalf("expected 0.1 loss ratio, got %f", hops[1].PacketLoss)
	}
	if hops[1].Received != 9 {
		t.Fatalf("expected 9 received, got %d", hops[1].Received)
	}

	// Third hop — unresponsive.
	if hops[2].IP != "" {
		t.Fatalf("expected empty IP for unresponsive hop, got %s", hops[2].IP)
	}
	if hops[2].PacketLoss != 1.0 {
		t.Fatalf("expected 1.0 loss for unresponsive hop, got %f", hops[2].PacketLoss)
	}

	// Last hop.
	if hops[4].IP != "8.8.8.8" {
		t.Fatalf("expected last hop IP 8.8.8.8, got %s", hops[4].IP)
	}
}

func TestParseMTRJSON_Invalid(t *testing.T) {
	_, err := parseMTRJSON([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Scamper JSON parsing tests ---

func TestParseScamperJSON(t *testing.T) {
	scamperOutput := `{"type":"cycle-start","list_name":"/dev/stdin","id":1,"hostname":"agent-01","start_time":1679000000}
{"type":"trace","src":"192.168.1.100","dst":"8.8.8.8","hops":[{"addr":"192.168.1.1","probe_ttl":1,"probes":[{"rtt":1.5,"reply":1},{"rtt":1.2,"reply":1}]},{"addr":"10.0.0.1","probe_ttl":2,"probes":[{"rtt":5.0,"reply":1},{"rtt":0,"reply":0}]},{"addr":"8.8.8.8","probe_ttl":3,"probes":[{"rtt":12.0,"reply":1},{"rtt":11.5,"reply":1}]}],"hop_count":3}
{"type":"cycle-stop","list_name":"/dev/stdin","id":1,"hostname":"agent-01","stop_time":1679000010}`

	hops, err := parseScamperJSON([]byte(scamperOutput))
	if err != nil {
		t.Fatal(err)
	}

	if len(hops) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(hops))
	}

	// First hop: 2 probes, both replied.
	if hops[0].IP != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %s", hops[0].IP)
	}
	if hops[0].RTTMin != 1.2 {
		t.Fatalf("expected min RTT 1.2, got %f", hops[0].RTTMin)
	}
	if hops[0].RTTMax != 1.5 {
		t.Fatalf("expected max RTT 1.5, got %f", hops[0].RTTMax)
	}
	if hops[0].PacketLoss != 0.0 {
		t.Fatalf("expected 0 loss, got %f", hops[0].PacketLoss)
	}

	// Second hop: 1 of 2 probes replied.
	if hops[1].PacketLoss != 0.5 {
		t.Fatalf("expected 0.5 loss, got %f", hops[1].PacketLoss)
	}
	if hops[1].Received != 1 {
		t.Fatalf("expected 1 received, got %d", hops[1].Received)
	}
}

func TestParseScamperJSON_NoTrace(t *testing.T) {
	_, err := parseScamperJSON([]byte(`{"type":"cycle-start"}`))
	if err == nil {
		t.Fatal("expected error when no trace object found")
	}
}

// --- Utility function tests ---

func TestBuildASPath(t *testing.T) {
	hops := []Hop{
		{HopNumber: 1, ASN: 64496},
		{HopNumber: 2, ASN: 64496}, // Duplicate — should be deduplicated.
		{HopNumber: 3, ASN: 0},     // Unknown — should be skipped.
		{HopNumber: 4, ASN: 15169},
		{HopNumber: 5, ASN: 15169},
		{HopNumber: 6, ASN: 13335},
	}

	path := buildASPath(hops)
	if len(path) != 3 {
		t.Fatalf("expected 3 ASNs, got %d: %v", len(path), path)
	}
	if path[0] != 64496 || path[1] != 15169 || path[2] != 13335 {
		t.Fatalf("unexpected AS path: %v", path)
	}
}

func TestBuildASPath_Empty(t *testing.T) {
	path := buildASPath([]Hop{{HopNumber: 1}, {HopNumber: 2}})
	if len(path) != 0 {
		t.Fatalf("expected empty AS path, got %v", path)
	}
}

func TestDidReachTarget_IP(t *testing.T) {
	hops := []Hop{
		{HopNumber: 1, IP: "192.168.1.1"},
		{HopNumber: 2, IP: "8.8.8.8"},
	}
	if !didReachTarget(hops, "8.8.8.8") {
		t.Fatal("expected to reach target 8.8.8.8")
	}
}

func TestDidReachTarget_NotReached(t *testing.T) {
	hops := []Hop{
		{HopNumber: 1, IP: "192.168.1.1"},
		{HopNumber: 2, IP: "10.0.0.1"},
	}
	if didReachTarget(hops, "8.8.8.8") {
		t.Fatal("should not have reached 8.8.8.8")
	}
}

func TestDidReachTarget_TrailingUnresponsive(t *testing.T) {
	hops := []Hop{
		{HopNumber: 1, IP: "192.168.1.1"},
		{HopNumber: 2, IP: "8.8.8.8"},
		{HopNumber: 3, IP: ""},   // Unresponsive trailing hop.
	}
	if !didReachTarget(hops, "8.8.8.8") {
		t.Fatal("should reach target even with trailing unresponsive hops")
	}
}

func TestDidReachTarget_Empty(t *testing.T) {
	if didReachTarget(nil, "8.8.8.8") {
		t.Fatal("should not reach target with no hops")
	}
}

func TestCanaryType(t *testing.T) {
	c := New()
	if c.Type() != "traceroute" {
		t.Fatalf("expected 'traceroute', got %s", c.Type())
	}
}

func TestMetricsJSON(t *testing.T) {
	m := Metrics{
		Backend:       "mtr",
		HopCount:      5,
		ReachedTarget: true,
		ASPath:        []int{64496, 15169},
		TotalMS:       1500.0,
		Hops: []Hop{
			{HopNumber: 1, IP: "192.168.1.1", RTTAvg: 1.5, PacketLoss: 0.0, Sent: 10},
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Metrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.HopCount != 5 || !decoded.ReachedTarget || len(decoded.ASPath) != 2 {
		t.Fatalf("unexpected decoded metrics: %+v", decoded)
	}
	if decoded.Hops[0].RTTAvg != 1.5 {
		t.Fatalf("expected hop RTT 1.5, got %f", decoded.Hops[0].RTTAvg)
	}
}

// --- MTR argument builder tests ---

func TestBuildMTRArgs(t *testing.T) {
	cfg := DefaultConfig()
	args := buildMTRArgs("8.8.8.8", cfg)

	// Should contain: --json --report -c 10 -m 30 -s 64 -i 0.25 8.8.8.8
	if args[0] != "--json" || args[1] != "--report" {
		t.Fatalf("expected --json --report, got %v", args[:2])
	}
	if args[len(args)-1] != "8.8.8.8" {
		t.Fatalf("expected target as last arg, got %s", args[len(args)-1])
	}
}

func TestBuildMTRArgs_TCP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Protocol = "tcp"
	cfg.Port = 443
	args := buildMTRArgs("example.com", cfg)

	found := false
	for _, a := range args {
		if a == "--tcp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --tcp in args: %v", args)
	}
}

func TestBuildMTRArgs_NoDNS(t *testing.T) {
	cfg := DefaultConfig()
	noDNS := false
	cfg.ResolveHostnames = &noDNS
	args := buildMTRArgs("8.8.8.8", cfg)

	found := false
	for _, a := range args {
		if a == "--no-dns" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --no-dns in args: %v", args)
	}
}

func TestMergeDefaults(t *testing.T) {
	cfg := Config{Backend: "scamper"} // Only set backend, rest should get defaults.
	merged := mergeDefaults(cfg)

	if merged.Backend != "scamper" {
		t.Fatalf("expected scamper, got %s", merged.Backend)
	}
	if merged.Cycles != 10 {
		t.Fatalf("expected 10 cycles, got %d", merged.Cycles)
	}
	if merged.MaxHops != 30 {
		t.Fatalf("expected 30 max_hops, got %d", merged.MaxHops)
	}
	if merged.ResolveASN == nil || !*merged.ResolveASN {
		t.Fatal("expected resolve_asn to default to true")
	}
}
