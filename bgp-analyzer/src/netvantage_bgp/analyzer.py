"""Core BGP Analyzer — subscribes to BGP streams and detects routing anomalies.

This is the main entry point for the BGP analysis engine. It:
  1. Subscribes to RouteViews/RIPE RIS via pybgpstream
  2. Filters for monitored prefixes
  3. Detects events: announcements, withdrawals, AS path changes, origin changes
  4. Detects hijacks: unexpected origin AS, MOAS conflicts, sub-prefix hijacks
  5. Validates RPKI status via Routinator HTTP API
  6. Exposes all events as Prometheus metrics
"""

import structlog
from prometheus_client import Counter, Gauge

from netvantage_bgp.config import AnalyzerConfig

logger = structlog.get_logger()

# Prometheus metrics — all prefixed with netvantage_bgp_
bgp_event_total = Counter(
    "netvantage_bgp_event_total",
    "Total BGP events observed",
    ["prefix", "event_type", "origin_asn", "peer_asn"],
)

bgp_analyzer_last_update = Gauge(
    "netvantage_bgp_analyzer_last_update",
    "Timestamp of last BGP update received",
    ["collector"],
)

bgp_rpki_status = Gauge(
    "netvantage_bgp_rpki_status",
    "RPKI ROV status per prefix announcement (1=current, 0=stale)",
    ["prefix", "origin_asn", "status"],
)

bgp_roa_expiry_days = Gauge(
    "netvantage_bgp_roa_expiry_days",
    "Days until ROA expiry for monitored prefixes",
    ["prefix"],
)


class BGPAnalyzer:
    """Main BGP analysis engine."""

    def __init__(self, config: AnalyzerConfig) -> None:
        self.config = config
        self.monitored_prefixes: set[str] = set(config.prefixes)
        # Track last-seen state per prefix for change detection.
        self._prefix_state: dict[str, dict] = {}
        logger.info(
            "bgp_analyzer_initialized",
            prefix_count=len(self.monitored_prefixes),
            collectors=config.collectors,
        )

    def run(self) -> None:
        """Start the BGP analysis loop. Blocks until stopped."""
        # TODO(M2): Implement pybgpstream subscription loop.
        # TODO(M2): Implement event detection logic.
        # TODO(M2): Implement RPKI validation via Routinator.
        # TODO(M2): Implement ROA lifecycle monitoring.
        logger.info("bgp_analyzer_starting")
        raise NotImplementedError("BGP stream subscription not yet implemented")

    def _handle_announcement(self, prefix: str, origin_asn: int, as_path: list[int], peer_asn: int, collector: str) -> None:
        """Process a BGP announcement for a monitored prefix."""
        # TODO(M2): Check for origin AS change → hijack detection.
        # TODO(M2): Check for MOAS conflict.
        # TODO(M2): Check for sub-prefix hijack.
        # TODO(M2): Validate RPKI status.
        bgp_event_total.labels(
            prefix=prefix,
            event_type="announcement",
            origin_asn=str(origin_asn),
            peer_asn=str(peer_asn),
        ).inc()

    def _handle_withdrawal(self, prefix: str, peer_asn: int, collector: str) -> None:
        """Process a BGP withdrawal for a monitored prefix."""
        bgp_event_total.labels(
            prefix=prefix,
            event_type="withdrawal",
            origin_asn="",
            peer_asn=str(peer_asn),
        ).inc()

    def _detect_hijack(self, prefix: str, observed_origin: int, expected_origin: int) -> bool:
        """Check if an announcement represents a potential hijack."""
        if expected_origin and observed_origin != expected_origin:
            logger.warning(
                "bgp_hijack_detected",
                prefix=prefix,
                expected_origin=expected_origin,
                observed_origin=observed_origin,
            )
            bgp_event_total.labels(
                prefix=prefix,
                event_type="hijack",
                origin_asn=str(observed_origin),
                peer_asn="",
            ).inc()
            return True
        return False
