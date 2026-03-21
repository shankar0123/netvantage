package http

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/agent/canary"
)

// --- Config tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Method != "GET" {
		t.Fatalf("expected GET, got %s", cfg.Method)
	}
	if cfg.ExpectedStatus != 200 {
		t.Fatalf("expected 200, got %d", cfg.ExpectedStatus)
	}
	if cfg.FollowRedirects == nil || !*cfg.FollowRedirects {
		t.Fatal("expected follow_redirects to be true")
	}
	if cfg.MaxRedirects != 10 {
		t.Fatalf("expected max_redirects 10, got %d", cfg.MaxRedirects)
	}
	if cfg.ValidateTLS == nil || !*cfg.ValidateTLS {
		t.Fatal("expected validate_tls to be true")
	}
}

func TestConfigUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		json string
		want func(Config) bool
	}{
		{
			"full config",
			`{"method":"POST","expected_status":201,"content_match":"ok","tls_skip_verify":true}`,
			func(c Config) bool {
				return c.Method == "POST" && c.ExpectedStatus == 201 && c.ContentMatch == "ok" && c.TLSSkipVerify
			},
		},
		{
			"headers",
			`{"headers":{"X-Custom":"value","Authorization":"Bearer tok"}}`,
			func(c Config) bool {
				return c.Headers["X-Custom"] == "value" && c.Headers["Authorization"] == "Bearer tok"
			},
		},
		{
			"body",
			`{"method":"POST","body":"{\"key\":\"value\"}"}`,
			func(c Config) bool { return c.Body == `{"key":"value"}` },
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
		{"GET is valid", `{"method":"GET"}`, false},
		{"POST is valid", `{"method":"POST"}`, false},
		{"HEAD is valid", `{"method":"HEAD"}`, false},
		{"lowercase is valid", `{"method":"get"}`, false},
		{"unsupported method", `{"method":"INVALID"}`, true},
		{"invalid regex", `{"content_regex":"[invalid"}`, true},
		{"negative max_redirects", `{"max_redirects":-1}`, true},
		{"invalid json", `not json`, true},
		{"valid regex", `{"content_regex":"^ok$"}`, false},
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

// --- Execute tests with httptest ---

func TestExecuteBasicGET(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	c := New()
	result, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:      "test-http-1",
		Type:    "http",
		Target:  srv.URL,
		Timeout: 5 * time.Second,
		Config:  json.RawMessage(`{}`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	var metrics Metrics
	if err := json.Unmarshal(result.Metrics, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", metrics.StatusCode)
	}
	if metrics.TotalMS <= 0 {
		t.Fatalf("expected positive total_ms, got %f", metrics.TotalMS)
	}
	if !metrics.ContentMatched {
		t.Fatal("expected content_matched to be true")
	}
}

func TestExecutePOSTWithBody(t *testing.T) {
	var receivedBody string
	var receivedMethod string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		receivedMethod = r.Method
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		receivedBody = string(body)
		w.WriteHeader(201)
		_, _ = w.Write([]byte("created"))
	}))
	defer srv.Close()

	c := New()
	result, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-http-2",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"method":"POST","body":"test-payload","expected_status":201}`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if receivedMethod != "POST" {
		t.Fatalf("expected POST, got %s", receivedMethod)
	}
	if receivedBody != "test-payload" {
		t.Fatalf("expected body 'test-payload', got '%s'", receivedBody)
	}
}

func TestExecuteStatusMismatch(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New()
	result, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-http-3",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"expected_status":200}`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected failure on status mismatch")
	}

	var metrics Metrics
	_ = json.Unmarshal(result.Metrics, &metrics)
	if metrics.StatusCode != 500 {
		t.Fatalf("expected status 500, got %d", metrics.StatusCode)
	}
}

func TestExecuteContentMatch(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = w.Write([]byte("the quick brown fox"))
	}))
	defer srv.Close()

	c := New()

	// Match found.
	result, _ := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-cm-1",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"content_match":"brown fox"}`),
	})
	if !result.Success {
		t.Fatal("expected content match success")
	}

	// Match not found.
	result, _ = c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-cm-2",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"content_match":"purple cow"}`),
	})
	if result.Success {
		t.Fatal("expected content match failure")
	}
}

func TestExecuteContentRegex(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","version":"1.2.3"}`))
	}))
	defer srv.Close()

	c := New()

	// Regex match.
	result, _ := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-re-1",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"content_regex":"\"version\":\"\\d+\\.\\d+\\.\\d+\""}`),
	})
	if !result.Success {
		t.Fatalf("expected regex match success, got: %s", result.Error)
	}

	// Regex no match.
	result, _ = c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-re-2",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"content_regex":"\"status\":\"failing\""}`),
	})
	if result.Success {
		t.Fatal("expected regex match failure")
	}
}

func TestExecuteCustomHeaders(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New()
	_, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-hdr-1",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"headers":{"Authorization":"Bearer my-token"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if receivedAuth != "Bearer my-token" {
		t.Fatalf("expected 'Bearer my-token', got '%s'", receivedAuth)
	}
}

func TestExecuteRedirects(t *testing.T) {
	redirectCount := 0
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/final" {
			_, _ = w.Write([]byte("done"))
			return
		}
		redirectCount++
		nethttp.Redirect(w, r, "/final", nethttp.StatusFound)
	}))
	defer srv.Close()

	c := New()
	result, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-redir",
		Type:   "http",
		Target: srv.URL + "/start",
		Config: json.RawMessage(`{}`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	var metrics Metrics
	_ = json.Unmarshal(result.Metrics, &metrics)
	if metrics.RedirectCount == 0 {
		t.Fatal("expected at least one redirect")
	}
}

func TestExecuteTLS(t *testing.T) {
	// Create a self-signed certificate for testing.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.netvantage.local"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"127.0.0.1", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	srv := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = w.Write([]byte("secure"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	c := New()
	result, err := c.Execute(context.Background(), canary.TestDefinition{
		ID:     "test-tls",
		Type:   "http",
		Target: srv.URL,
		Config: json.RawMessage(`{"tls_skip_verify":true}`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	var metrics Metrics
	_ = json.Unmarshal(result.Metrics, &metrics)
	if metrics.TLSVersion == "" {
		t.Fatal("expected TLS version to be set")
	}
	if metrics.CertSubject == "" {
		t.Fatal("expected cert subject to be set")
	}
	if metrics.CertExpiryDays <= 0 {
		t.Fatalf("expected positive cert expiry days, got %f", metrics.CertExpiryDays)
	}
}

func TestCanaryType(t *testing.T) {
	c := New()
	if c.Type() != "http" {
		t.Fatalf("expected 'http', got %s", c.Type())
	}
}

func TestMetricsJSON(t *testing.T) {
	m := Metrics{
		DNSMS:          1.5,
		TCPMS:          2.0,
		TLSMS:          10.0,
		TTFBMS:         15.0,
		TotalMS:        25.0,
		StatusCode:     200,
		ContentMatched: true,
		CertExpiryDays: 364.5,
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Metrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TotalMS != 25.0 || decoded.StatusCode != 200 || !decoded.ContentMatched {
		t.Fatalf("unexpected decoded metrics: %+v", decoded)
	}
}

// --- Helper tests ---

func TestTLSVersionString(t *testing.T) {
	cases := []struct {
		version uint16
		want    string
	}{
		{tls.VersionTLS10, "TLS 1.0"},
		{tls.VersionTLS11, "TLS 1.1"},
		{tls.VersionTLS12, "TLS 1.2"},
		{tls.VersionTLS13, "TLS 1.3"},
		{0x0000, "0x0000"},
	}

	for _, tc := range cases {
		got := tlsVersionString(tc.version)
		if got != tc.want {
			t.Fatalf("version 0x%04x: expected %s, got %s", tc.version, tc.want, got)
		}
	}
}

func TestMsElapsed(t *testing.T) {
	start := time.Now()
	end := start.Add(1500 * time.Microsecond)
	got := msElapsed(start, end)
	if got != 1.5 {
		t.Fatalf("expected 1.5ms, got %f", got)
	}
}
