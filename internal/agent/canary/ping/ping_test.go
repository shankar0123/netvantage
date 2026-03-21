package ping

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Count != 5 {
		t.Errorf("expected Count=5, got %d", cfg.Count)
	}
	if cfg.Interval != 200*time.Millisecond {
		t.Errorf("expected Interval=200ms, got %v", cfg.Interval)
	}
	if cfg.PayloadSize != 56 {
		t.Errorf("expected PayloadSize=56, got %d", cfg.PayloadSize)
	}
	if !cfg.Privileged {
		t.Error("expected Privileged=true")
	}
}

func TestConfigUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Config
		wantErr bool
	}{
		{
			name:  "all fields",
			input: `{"count":10,"interval":500000000,"payload_size":128,"privileged":false}`,
			want: Config{
				Count:       10,
				Interval:    500 * time.Millisecond,
				PayloadSize: 128,
				Privileged:  false,
			},
		},
		{
			name:  "partial fields use zero values",
			input: `{"count":3}`,
			want: Config{
				Count: 3,
			},
		},
		{
			name:    "invalid json",
			input:   `{not json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Config
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestValidate(t *testing.T) {
	c := New()

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			config:  "",
			wantErr: false,
		},
		{
			name:    "defaults are valid",
			config:  `{"count":5,"payload_size":56}`,
			wantErr: false,
		},
		{
			name:    "count zero is invalid",
			config:  `{"count":0}`,
			wantErr: true,
		},
		{
			name:    "negative count is invalid",
			config:  `{"count":-1}`,
			wantErr: true,
		},
		{
			name:    "negative payload is invalid",
			config:  `{"payload_size":-1}`,
			wantErr: true,
		},
		{
			name:    "oversized payload is invalid",
			config:  `{"payload_size":70000}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			config:  `{broken`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Validate(json.RawMessage(tt.config))
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%s) error = %v, wantErr %v", tt.config, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Jitter computation tests
// ---------------------------------------------------------------------------

func TestComputeJitter(t *testing.T) {
	tests := []struct {
		name string
		rtts []time.Duration
		want float64
	}{
		{
			name: "no samples",
			rtts: nil,
			want: 0,
		},
		{
			name: "single sample",
			rtts: []time.Duration{10 * time.Millisecond},
			want: 0,
		},
		{
			name: "constant RTT has zero jitter",
			rtts: []time.Duration{
				10 * time.Millisecond,
				10 * time.Millisecond,
				10 * time.Millisecond,
			},
			want: 0,
		},
		{
			name: "alternating RTT",
			rtts: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				10 * time.Millisecond,
				20 * time.Millisecond,
			},
			want: 10, // |20-10| + |10-20| + |20-10| = 30 / 3 = 10ms
		},
		{
			name: "increasing RTT",
			rtts: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
			},
			want: 10, // |20-10| + |30-20| = 20 / 2 = 10ms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &probing.Statistics{Rtts: tt.rtts}
			got := computeJitter(stats)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("computeJitter() = %f, want %f", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Metrics serialization tests
// ---------------------------------------------------------------------------

func TestMetricsJSON(t *testing.T) {
	m := Metrics{
		RTTMin:      1.23,
		RTTAvg:      4.56,
		RTTMax:      7.89,
		RTTStdDev:   2.34,
		PacketLoss:  0.2,
		Jitter:      1.11,
		PacketsSent: 5,
		PacketsRecv: 4,
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Metrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != m {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, m)
	}
}

func TestCanaryType(t *testing.T) {
	c := New()
	if c.Type() != "ping" {
		t.Errorf("Type() = %q, want %q", c.Type(), "ping")
	}
}
