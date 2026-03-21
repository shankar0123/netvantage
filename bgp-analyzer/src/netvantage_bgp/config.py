"""Configuration for the BGP Analyzer."""

from dataclasses import dataclass, field


@dataclass
class AnalyzerConfig:
    """BGP Analyzer configuration."""

    # List of IP prefixes to monitor (IPv4 and IPv6).
    # Example: ["1.2.3.0/24", "2001:db8::/32"]
    prefixes: list[str] = field(default_factory=list)

    # Expected origin ASN per prefix. Used for hijack detection.
    # Key: prefix string, Value: expected origin ASN.
    expected_origins: dict[str, int] = field(default_factory=dict)

    # BGP collectors to subscribe to. Default: all RouteViews + RIPE RIS.
    collectors: list[str] = field(default_factory=lambda: ["routeviews", "ris"])

    # Routinator HTTP API URL for RPKI validation.
    routinator_url: str = "http://localhost:8323"

    # Prometheus pushgateway or HTTP server port for metrics.
    metrics_port: int = 9100

    # Staleness threshold: alert if no BGP updates received within this
    # many seconds. Default: 300 (5 minutes).
    staleness_threshold_seconds: int = 300

    # ROA expiry warning thresholds in days.
    roa_expiry_warning_days: list[int] = field(default_factory=lambda: [30, 14, 7, 1])

    # NATS URL for publishing BGP path updates (used by M8 correlation engine).
    # Set to empty string to disable NATS publishing (correlation unavailable).
    nats_url: str = "nats://localhost:4222"

    # Log level.
    log_level: str = "info"
