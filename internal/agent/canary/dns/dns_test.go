package dns

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RecordType != "A" {
		t.Errorf("expected RecordType=A, got %q", cfg.RecordType)
	}
	if len(cfg.Resolvers) != 0 {
		t.Errorf("expected empty Resolvers, got %v", cfg.Resolvers)
	}
	if len(cfg.ExpectedValues) != 0 {
		t.Errorf("expected empty ExpectedValues, got %v", cfg.ExpectedValues)
	}
}

func TestConfigUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "full config",
			input: `{"record_type":"AAAA","resolvers":["8.8.8.8","1.1.1.1"],"expected_values":["2606:2800:220:1::"]}`,
		},
		{
			name:  "partial config",
			input: `{"record_type":"MX"}`,
		},
		{
			name:    "invalid json",
			input:   `{broken`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tt.input), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
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
			name:    "A record is valid",
			config:  `{"record_type":"A"}`,
			wantErr: false,
		},
		{
			name:    "AAAA record is valid",
			config:  `{"record_type":"AAAA"}`,
			wantErr: false,
		},
		{
			name:    "MX record is valid",
			config:  `{"record_type":"MX"}`,
			wantErr: false,
		},
		{
			name:    "lowercase is valid (case insensitive)",
			config:  `{"record_type":"txt"}`,
			wantErr: false,
		},
		{
			name:    "unsupported record type",
			config:  `{"record_type":"PTR"}`,
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
// DNS error classification tests
// ---------------------------------------------------------------------------

func TestClassifyDNSError(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		want    string
	}{
		{"nil error", "", "NOERROR"},
		{"nxdomain", "lookup example.invalid: no such host", "NXDOMAIN"},
		{"servfail", "read: server misbehaving", "SERVFAIL"},
		{"timeout", "read: i/o timeout", "TIMEOUT"},
		{"refused", "dial: connection refused", "REFUSED"},
		{"unknown", "some other error", "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf(tt.errMsg)
			}
			got := classifyDNSError(err)
			if got != tt.want {
				t.Errorf("classifyDNSError(%q) = %q, want %q", tt.errMsg, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Content validation tests
// ---------------------------------------------------------------------------

func TestValidateContent(t *testing.T) {
	tests := []struct {
		name     string
		resolved []string
		expected []string
		want     bool
	}{
		{
			name:     "all expected present",
			resolved: []string{"1.2.3.4", "5.6.7.8"},
			expected: []string{"1.2.3.4"},
			want:     true,
		},
		{
			name:     "exact match",
			resolved: []string{"1.2.3.4"},
			expected: []string{"1.2.3.4"},
			want:     true,
		},
		{
			name:     "missing expected value",
			resolved: []string{"1.2.3.4"},
			expected: []string{"5.6.7.8"},
			want:     false,
		},
		{
			name:     "empty expected always passes",
			resolved: []string{"1.2.3.4"},
			expected: nil,
			want:     true,
		},
		{
			name:     "empty resolved fails non-empty expected",
			resolved: nil,
			expected: []string{"1.2.3.4"},
			want:     false,
		},
		{
			name:     "multiple expected all present",
			resolved: []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"},
			expected: []string{"1.2.3.4", "9.10.11.12"},
			want:     true,
		},
		{
			name:     "multiple expected one missing",
			resolved: []string{"1.2.3.4", "5.6.7.8"},
			expected: []string{"1.2.3.4", "9.10.11.12"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateContent(tt.resolved, tt.expected)
			if got != tt.want {
				t.Errorf("validateContent(%v, %v) = %v, want %v",
					tt.resolved, tt.expected, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Metrics serialization tests
// ---------------------------------------------------------------------------

func TestMetricsJSON(t *testing.T) {
	m := Metrics{
		RecordType:          "A",
		AvgResolutionTimeMS: 12.5,
		Resolvers: []ResolverResult{
			{
				Resolver:         "8.8.8.8",
				ResolutionTimeMS: 10.0,
				ResponseCode:     "NOERROR",
				ResolvedValues:   []string{"93.184.216.34"},
				Success:          true,
			},
			{
				Resolver:         "1.1.1.1",
				ResolutionTimeMS: 15.0,
				ResponseCode:     "NOERROR",
				ResolvedValues:   []string{"93.184.216.34"},
				Success:          true,
			},
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Metrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.RecordType != m.RecordType {
		t.Errorf("RecordType mismatch: got %q, want %q", decoded.RecordType, m.RecordType)
	}
	if len(decoded.Resolvers) != 2 {
		t.Errorf("expected 2 resolvers, got %d", len(decoded.Resolvers))
	}
}

func TestCanaryType(t *testing.T) {
	c := New()
	if c.Type() != "dns" {
		t.Errorf("Type() = %q, want %q", c.Type(), "dns")
	}
}
