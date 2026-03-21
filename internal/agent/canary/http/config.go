package http

import "time"

// Config holds HTTP canary-specific configuration.
type Config struct {
	// Method is the HTTP method (GET, POST, HEAD). Default: GET.
	Method string `json:"method,omitempty"`

	// Headers are custom HTTP headers to send.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is the request body (for POST/PUT).
	Body string `json:"body,omitempty"`

	// ExpectedStatus is the expected HTTP status code. Default: 200.
	ExpectedStatus int `json:"expected_status,omitempty"`

	// ContentMatch is a string that must appear in the response body.
	ContentMatch string `json:"content_match,omitempty"`

	// ContentRegex is a regex pattern the response body must match.
	ContentRegex string `json:"content_regex,omitempty"`

	// FollowRedirects controls whether redirects are followed. Default: true.
	FollowRedirects *bool `json:"follow_redirects,omitempty"`

	// MaxRedirects is the maximum number of redirects to follow. Default: 10.
	MaxRedirects int `json:"max_redirects,omitempty"`

	// TLSSkipVerify disables TLS certificate validation. Default: false.
	TLSSkipVerify bool `json:"tls_skip_verify,omitempty"`

	// ValidateTLS enables TLS certificate expiry checking. Default: true.
	ValidateTLS *bool `json:"validate_tls,omitempty"`
}

// DefaultConfig returns sensible defaults for the HTTP canary.
func DefaultConfig() Config {
	followRedirects := true
	validateTLS := true
	return Config{
		Method:          "GET",
		ExpectedStatus:  200,
		FollowRedirects: &followRedirects,
		MaxRedirects:    10,
		ValidateTLS:     &validateTLS,
	}
}

// Metrics holds the timing breakdown and metadata from an HTTP check.
type Metrics struct {
	// Timing breakdown in milliseconds.
	DNSMS     float64 `json:"dns_ms"`
	TCPMS     float64 `json:"tcp_ms"`
	TLSMS     float64 `json:"tls_ms"`
	TTFBMS    float64 `json:"ttfb_ms"`
	TotalMS   float64 `json:"total_ms"`
	TransferMS float64 `json:"transfer_ms"`

	// Response metadata.
	StatusCode    int    `json:"status_code"`
	ContentLength int64  `json:"content_length"`
	Protocol      string `json:"protocol,omitempty"`

	// TLS certificate info (HTTPS only).
	TLSVersion    string  `json:"tls_version,omitempty"`
	TLSCipher     string  `json:"tls_cipher,omitempty"`
	CertSubject   string  `json:"cert_subject,omitempty"`
	CertIssuer    string  `json:"cert_issuer,omitempty"`
	CertExpiryDays float64 `json:"cert_expiry_days,omitempty"`

	// Redirect chain (if redirects occurred).
	RedirectCount int      `json:"redirect_count"`
	RedirectChain []string `json:"redirect_chain,omitempty"`

	// Content validation.
	ContentMatched bool `json:"content_matched"`
}

// TimingTrace captures HTTP request phase timestamps.
type TimingTrace struct {
	DNSStart     time.Time
	DNSDone      time.Time
	ConnectStart time.Time
	ConnectDone  time.Time
	TLSStart     time.Time
	TLSDone      time.Time
	GotFirstByte time.Time
	RequestStart time.Time
	RequestDone  time.Time
}
