package kafka

import (
	"testing"

	"github.com/IBM/sarama"
)

func TestDefaultConfig(t *testing.T) {
	brokers := []string{"localhost:9092", "localhost:9093"}
	cfg := DefaultConfig(brokers)

	if len(cfg.Brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(cfg.Brokers))
	}
	if cfg.TopicPrefix != "netvantage" {
		t.Errorf("expected topic prefix 'netvantage', got %q", cfg.TopicPrefix)
	}
	if cfg.ConsumerGroup != "netvantage-processor" {
		t.Errorf("expected consumer group 'netvantage-processor', got %q", cfg.ConsumerGroup)
	}
	if cfg.RequiredAcks != sarama.WaitForAll {
		t.Errorf("expected RequiredAcks WaitForAll, got %v", cfg.RequiredAcks)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 max retries, got %d", cfg.MaxRetries)
	}
	if cfg.InitialOffset != sarama.OffsetNewest {
		t.Errorf("expected OffsetNewest, got %d", cfg.InitialOffset)
	}
}

func TestFullTopic(t *testing.T) {
	tr := &Transport{cfg: Config{TopicPrefix: "netvantage"}}

	tests := []struct {
		input string
		want  string
	}{
		{"netvantage.ping.results", "netvantage-ping-results"},
		{"netvantage.dns.results", "netvantage-dns-results"},
		{"netvantage.http.results", "netvantage-http-results"},
		{"netvantage.traceroute.results", "netvantage-traceroute-results"},
		{"netvantage.bgp.paths", "netvantage-bgp-paths"},
		{"no-dots-here", "no-dots-here"},
	}

	for _, tt := range tests {
		got := tr.fullTopic(tt.input)
		if got != tt.want {
			t.Errorf("fullTopic(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSaramaConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sc.Producer.Return.Successes {
		t.Error("expected Producer.Return.Successes to be true")
	}
	if sc.Net.SASL.Enable {
		t.Error("expected SASL to be disabled by default")
	}
	if sc.Net.TLS.Enable {
		t.Error("expected TLS to be disabled by default")
	}
	if sc.ClientID != "netvantage" {
		t.Errorf("expected client ID 'netvantage', got %q", sc.ClientID)
	}
}

func TestBuildSaramaConfig_SASL_SHA256(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	cfg.SASLEnabled = true
	cfg.SASLMechanism = "SCRAM-SHA-256"
	cfg.SASLUsername = "user"
	cfg.SASLPassword = "pass"

	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sc.Net.SASL.Enable {
		t.Error("expected SASL to be enabled")
	}
	if sc.Net.SASL.User != "user" {
		t.Errorf("expected SASL user 'user', got %q", sc.Net.SASL.User)
	}
	if sc.Net.SASL.Mechanism != sarama.SASLTypeSCRAMSHA256 {
		t.Errorf("expected SCRAM-SHA-256 mechanism, got %v", sc.Net.SASL.Mechanism)
	}
}

func TestBuildSaramaConfig_SASL_SHA512(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	cfg.SASLEnabled = true
	cfg.SASLMechanism = "SCRAM-SHA-512"
	cfg.SASLUsername = "admin"
	cfg.SASLPassword = "secret"

	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sc.Net.SASL.Mechanism != sarama.SASLTypeSCRAMSHA512 {
		t.Errorf("expected SCRAM-SHA-512 mechanism, got %v", sc.Net.SASL.Mechanism)
	}
}

func TestBuildSaramaConfig_InvalidSASLMechanism(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	cfg.SASLEnabled = true
	cfg.SASLMechanism = "PLAIN"

	_, err := buildSaramaConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported SASL mechanism")
	}
}

func TestBuildSaramaConfig_TLS(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	cfg.TLSEnabled = true
	cfg.TLSSkipVerify = true

	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sc.Net.TLS.Enable {
		t.Error("expected TLS to be enabled")
	}
	if !sc.Net.TLS.Config.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildSaramaConfig_TLS_InvalidCACert(t *testing.T) {
	cfg := DefaultConfig([]string{"localhost:9092"})
	cfg.TLSEnabled = true
	cfg.TLSCACert = "/nonexistent/ca.crt"

	_, err := buildSaramaConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA cert")
	}
}

func TestNewTransport_NoBrokers(t *testing.T) {
	cfg := Config{}
	_, err := New(cfg, nil)
	if err == nil {
		t.Fatal("expected error for empty brokers")
	}
}

func TestScramClient(t *testing.T) {
	sc := &scramClient{mechanism: "SCRAM-SHA-256"}
	if err := sc.Begin("user", "pass", ""); err != nil {
		t.Errorf("Begin() error: %v", err)
	}
	resp, err := sc.Step("challenge")
	if err != nil {
		t.Errorf("Step() error: %v", err)
	}
	if resp != "" {
		t.Errorf("expected empty response, got %q", resp)
	}
	if !sc.Done() {
		t.Error("expected Done() to be true")
	}
}
