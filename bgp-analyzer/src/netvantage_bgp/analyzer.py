"""Core BGP Analyzer — subscribes to BGP streams and detects routing anomalies.

This is the main engine for the BGP analysis service. It:
  1. Subscribes to RouteViews/RIPE RIS via pybgpstream
  2. Filters for monitored prefixes (including sub-prefix detection)
  3. Detects events: announcements, withdrawals, AS path changes, origin changes
  4. Detects hijacks: unexpected origin AS, MOAS conflicts, sub-prefix hijacks
  5. Validates RPKI status via Routinator HTTP API
  6. Monitors ROA lifecycle (expiry warnings)
  7. Exposes all events as Prometheus metrics
"""

import threading
import time
from datetime import datetime, timezone

import structlog
from prometheus_client import Counter, Gauge

from netvantage_bgp.config import AnalyzerConfig
from netvantage_bgp.prefix import is_sub_prefix, matches_any_monitored
from netvantage_bgp.publisher import SyncBGPPathPublisher
from netvantage_bgp.rpki import RPKIStatus, RPKIValidator

logger = structlog.get_logger()

# ---------------------------------------------------------------------------
# Prometheus metrics — all prefixed with netvantage_bgp_
# ---------------------------------------------------------------------------

bgp_event_total = Counter(
    "netvantage_bgp_event_total",
    "Total BGP events observed",
    ["prefix", "event_type", "origin_asn", "peer_asn"],
)

bgp_hijack_total = Counter(
    "netvantage_bgp_hijack_total",
    "Total BGP hijack events detected",
    ["prefix", "expected_origin", "observed_origin", "hijack_type"],
)

bgp_analyzer_last_update = Gauge(
    "netvantage_bgp_analyzer_last_update",
    "Unix timestamp of last BGP update received",
    ["collector"],
)

bgp_rpki_status = Gauge(
    "netvantage_bgp_rpki_status",
    "RPKI ROV status per prefix/origin (1=active, 0=stale)",
    ["prefix", "origin_asn", "status"],
)

bgp_roa_expiry_days = Gauge(
    "netvantage_bgp_roa_expiry_days",
    "Days until ROA expiry for monitored prefixes",
    ["prefix", "asn"],
)

bgp_updates_processed = Counter(
    "netvantage_bgp_updates_processed_total",
    "Total BGP updates processed (including filtered-out)",
)

bgp_stream_errors = Counter(
    "netvantage_bgp_stream_errors_total",
    "Errors encountered reading from BGP stream",
    ["error_type"],
)


class PrefixState:
    """Tracks the last-known state of a monitored prefix from each peer."""

    def __init__(self) -> None:
        # Key: (prefix, peer_asn) → dict with origin_asn, as_path, last_seen
        self.announcements: dict[tuple[str, int], dict] = {}
        # Key: prefix → set of origin ASNs currently announcing it
        self.active_origins: dict[str, set[int]] = {}

    def record_announcement(
        self, prefix: str, origin_asn: int, as_path: list[int], peer_asn: int
    ) -> dict:
        """Record an announcement and return change info.

        Returns a dict with keys:
          - is_new: bool — first time seeing this prefix from this peer
          - origin_changed: bool — origin AS changed from previous
          - path_changed: bool — AS path changed from previous
          - previous_origin: int | None — previous origin AS if changed
          - previous_path: list[int] | None — previous path if changed
        """
        key = (prefix, peer_asn)
        prev = self.announcements.get(key)

        changes = {
            "is_new": prev is None,
            "origin_changed": False,
            "path_changed": False,
            "previous_origin": None,
            "previous_path": None,
        }

        if prev is not None:
            if prev["origin_asn"] != origin_asn:
                changes["origin_changed"] = True
                changes["previous_origin"] = prev["origin_asn"]
            if prev["as_path"] != as_path:
                changes["path_changed"] = True
                changes["previous_path"] = prev["as_path"]

        self.announcements[key] = {
            "origin_asn": origin_asn,
            "as_path": as_path,
            "last_seen": time.time(),
        }

        # Track active origins for MOAS detection.
        if prefix not in self.active_origins:
            self.active_origins[prefix] = set()
        self.active_origins[prefix].add(origin_asn)

        return changes

    def record_withdrawal(self, prefix: str, peer_asn: int) -> dict | None:
        """Record a withdrawal. Returns the previous state if it existed."""
        key = (prefix, peer_asn)
        prev = self.announcements.pop(key, None)

        # Rebuild active origins for this prefix from remaining announcements.
        remaining = set()
        for (p, _), state in self.announcements.items():
            if p == prefix:
                remaining.add(state["origin_asn"])
        self.active_origins[prefix] = remaining

        return prev

    def get_active_origins(self, prefix: str) -> set[int]:
        """Get the set of origin ASNs currently announcing a prefix."""
        return self.active_origins.get(prefix, set())


class BGPAnalyzer:
    """Main BGP analysis engine."""

    def __init__(self, config: AnalyzerConfig) -> None:
        self.config = config
        self.monitored_prefixes: set[str] = set(config.prefixes)
        self.expected_origins: dict[str, int] = dict(config.expected_origins)
        self.state = PrefixState()
        self.rpki = RPKIValidator(config.routinator_url)
        self._stop_event = threading.Event()
        self._roa_thread: threading.Thread | None = None

        # NATS publisher for BGP+Traceroute correlation (M8).
        self._publisher: SyncBGPPathPublisher | None = None
        if config.nats_url:
            self._publisher = SyncBGPPathPublisher(config.nats_url)

        logger.info(
            "bgp_analyzer_initialized",
            prefix_count=len(self.monitored_prefixes),
            expected_origins_count=len(self.expected_origins),
            collectors=config.collectors,
            routinator_url=config.routinator_url,
        )

    def run(self) -> None:
        """Start the BGP analysis loop. Blocks until stopped."""
        logger.info("bgp_analyzer_starting")

        # Connect NATS publisher for path correlation.
        if self._publisher:
            self._publisher.connect()

        # Start ROA lifecycle monitor in background thread.
        self._roa_thread = threading.Thread(
            target=self._roa_monitor_loop, daemon=True, name="roa-monitor"
        )
        self._roa_thread.start()

        self._stream_loop()

    def stop(self) -> None:
        """Signal the analyzer to stop."""
        logger.info("bgp_analyzer_stopping")
        self._stop_event.set()
        if self._publisher:
            self._publisher.close()

    def _stream_loop(self) -> None:
        """Main BGP stream consumption loop using pybgpstream."""
        try:
            import pybgpstream
        except ImportError:
            logger.error(
                "pybgpstream_not_installed",
                hint="Install with: pip install pybgpstream",
            )
            raise

        stream = pybgpstream.BGPStream(
            project="ris-live" if "ris" in self.config.collectors else "routeviews-stream",
            record_type="updates",
            filter="prefix any",
        )

        # Apply prefix filters if pybgpstream supports it.
        # We also filter in code for sub-prefix detection.
        for prefix in self.monitored_prefixes:
            try:
                stream.add_filter("prefix-any", prefix)
            except Exception:
                # Some pybgpstream versions have different filter APIs.
                pass

        logger.info("bgp_stream_connected", collectors=self.config.collectors)

        for elem in stream:
            if self._stop_event.is_set():
                break

            bgp_updates_processed.inc()

            try:
                self._process_element(elem)
            except Exception as e:
                bgp_stream_errors.labels(error_type=type(e).__name__).inc()
                logger.warning(
                    "bgp_element_processing_error",
                    error=str(e),
                    elem_type=getattr(elem, "type", "unknown"),
                )

    def _process_element(self, elem) -> None:
        """Process a single BGP stream element."""
        prefix = elem.fields.get("prefix", "")
        if not prefix:
            return

        # Check if this prefix matches any of our monitored prefixes.
        matched_monitored = matches_any_monitored(prefix, self.monitored_prefixes)
        if matched_monitored is None:
            return

        collector = getattr(elem, "collector", "unknown")
        peer_asn = int(elem.peer_asn) if hasattr(elem, "peer_asn") else 0

        # Update staleness tracker.
        bgp_analyzer_last_update.labels(collector=collector).set_to_current_time()

        elem_type = elem.type

        if elem_type == "A":  # Announcement
            as_path_str = elem.fields.get("as-path", "")
            as_path = self._parse_as_path(as_path_str)
            origin_asn = as_path[-1] if as_path else 0

            self._handle_announcement(
                prefix=prefix,
                origin_asn=origin_asn,
                as_path=as_path,
                peer_asn=peer_asn,
                collector=collector,
                matched_monitored=matched_monitored,
            )

        elif elem_type == "W":  # Withdrawal
            self._handle_withdrawal(
                prefix=prefix,
                peer_asn=peer_asn,
                collector=collector,
                matched_monitored=matched_monitored,
            )

    def _handle_announcement(
        self,
        prefix: str,
        origin_asn: int,
        as_path: list[int],
        peer_asn: int,
        collector: str,
        matched_monitored: str,
    ) -> None:
        """Process a BGP announcement for a monitored prefix."""
        # Record state and detect changes.
        changes = self.state.record_announcement(prefix, origin_asn, as_path, peer_asn)

        # Emit base announcement metric.
        bgp_event_total.labels(
            prefix=prefix,
            event_type="announcement",
            origin_asn=str(origin_asn),
            peer_asn=str(peer_asn),
        ).inc()

        # Log origin change events.
        if changes["origin_changed"]:
            bgp_event_total.labels(
                prefix=prefix,
                event_type="origin_change",
                origin_asn=str(origin_asn),
                peer_asn=str(peer_asn),
            ).inc()
            logger.warning(
                "bgp_origin_change",
                prefix=prefix,
                previous_origin=changes["previous_origin"],
                new_origin=origin_asn,
                peer_asn=peer_asn,
                collector=collector,
            )

        # Log AS path change events.
        if changes["path_changed"] and not changes["origin_changed"]:
            bgp_event_total.labels(
                prefix=prefix,
                event_type="path_change",
                origin_asn=str(origin_asn),
                peer_asn=str(peer_asn),
            ).inc()

        # --- Hijack Detection ---
        self._check_hijack(prefix, origin_asn, matched_monitored, collector)

        # --- MOAS Detection ---
        self._check_moas(prefix, origin_asn, collector)

        # --- Sub-prefix Hijack Detection ---
        if prefix != matched_monitored and is_sub_prefix(matched_monitored, prefix):
            expected = self.expected_origins.get(matched_monitored)
            if expected is not None and origin_asn != expected:
                bgp_hijack_total.labels(
                    prefix=prefix,
                    expected_origin=str(expected),
                    observed_origin=str(origin_asn),
                    hijack_type="sub_prefix",
                ).inc()
                logger.critical(
                    "bgp_sub_prefix_hijack",
                    announced_prefix=prefix,
                    parent_prefix=matched_monitored,
                    expected_origin=expected,
                    observed_origin=origin_asn,
                    collector=collector,
                )

        # --- RPKI Validation ---
        self._validate_rpki(prefix, origin_asn, collector)

        # --- Publish to NATS for BGP+Traceroute Correlation (M8) ---
        if self._publisher:
            self._publisher.publish_announcement(
                prefix=prefix,
                origin_asn=origin_asn,
                as_path=as_path,
                peer_asn=peer_asn,
                collector=collector,
            )

    def _handle_withdrawal(
        self,
        prefix: str,
        peer_asn: int,
        collector: str,
        matched_monitored: str,
    ) -> None:
        """Process a BGP withdrawal for a monitored prefix."""
        prev = self.state.record_withdrawal(prefix, peer_asn)

        bgp_event_total.labels(
            prefix=prefix,
            event_type="withdrawal",
            origin_asn=str(prev["origin_asn"]) if prev else "",
            peer_asn=str(peer_asn),
        ).inc()

        logger.info(
            "bgp_withdrawal",
            prefix=prefix,
            peer_asn=peer_asn,
            collector=collector,
            had_previous_state=prev is not None,
        )

        # Publish to NATS for BGP+Traceroute Correlation (M8).
        if self._publisher:
            self._publisher.publish_withdrawal(
                prefix=prefix,
                peer_asn=peer_asn,
                collector=collector,
            )

    def _check_hijack(
        self, prefix: str, origin_asn: int, matched_monitored: str, collector: str
    ) -> None:
        """Check if announcement is from an unexpected origin AS."""
        expected = self.expected_origins.get(matched_monitored)
        if expected is None:
            return

        if origin_asn != expected:
            bgp_hijack_total.labels(
                prefix=prefix,
                expected_origin=str(expected),
                observed_origin=str(origin_asn),
                hijack_type="origin",
            ).inc()
            bgp_event_total.labels(
                prefix=prefix,
                event_type="hijack",
                origin_asn=str(origin_asn),
                peer_asn="",
            ).inc()
            logger.critical(
                "bgp_hijack_detected",
                prefix=prefix,
                expected_origin=expected,
                observed_origin=origin_asn,
                collector=collector,
            )

    def _check_moas(self, prefix: str, origin_asn: int, collector: str) -> None:
        """Check for Multiple Origin AS (MOAS) conflict."""
        active = self.state.get_active_origins(prefix)
        if len(active) > 1:
            bgp_hijack_total.labels(
                prefix=prefix,
                expected_origin="multiple",
                observed_origin=str(origin_asn),
                hijack_type="moas",
            ).inc()
            logger.warning(
                "bgp_moas_conflict",
                prefix=prefix,
                active_origins=sorted(active),
                latest_origin=origin_asn,
                collector=collector,
            )

    def _validate_rpki(self, prefix: str, origin_asn: int, collector: str) -> None:
        """Validate announcement against RPKI via Routinator."""
        result = self.rpki.validate(prefix, origin_asn)

        # Set the gauge: 1 for the current status, implicitly 0 for others.
        bgp_rpki_status.labels(
            prefix=prefix,
            origin_asn=str(origin_asn),
            status=result.status.value,
        ).set(1)

        if result.status == RPKIStatus.INVALID:
            bgp_event_total.labels(
                prefix=prefix,
                event_type="rpki_invalid",
                origin_asn=str(origin_asn),
                peer_asn="",
            ).inc()
            logger.critical(
                "bgp_rpki_invalid_announcement",
                prefix=prefix,
                origin_asn=origin_asn,
                rpki_description=result.description,
                collector=collector,
            )

    def _roa_monitor_loop(self) -> None:
        """Periodically check ROA expiry for monitored prefixes.

        Runs in a background thread. Checks every hour by default.
        """
        check_interval = 3600  # 1 hour
        logger.info("roa_monitor_started", check_interval_seconds=check_interval)

        while not self._stop_event.is_set():
            try:
                self._check_roa_expiry()
            except Exception as e:
                logger.warning("roa_monitor_error", error=str(e))

            self._stop_event.wait(timeout=check_interval)

    def _check_roa_expiry(self) -> None:
        """Check ROA expiry for all monitored prefixes."""
        now = datetime.now(timezone.utc)

        for prefix in self.monitored_prefixes:
            roas = self.rpki.get_roas_for_prefix(prefix)

            if not roas:
                logger.info("no_roas_found", prefix=prefix)
                continue

            for roa in roas:
                if not roa.not_after:
                    continue

                try:
                    expiry = datetime.fromisoformat(roa.not_after.replace("Z", "+00:00"))
                    days_until = (expiry - now).days

                    bgp_roa_expiry_days.labels(
                        prefix=roa.prefix,
                        asn=str(roa.asn),
                    ).set(days_until)

                    for threshold in self.config.roa_expiry_warning_days:
                        if days_until <= threshold:
                            logger.warning(
                                "roa_expiry_approaching",
                                prefix=roa.prefix,
                                asn=roa.asn,
                                days_until_expiry=days_until,
                                expiry_date=roa.not_after,
                                threshold_days=threshold,
                            )
                            break  # Only log the most urgent threshold.

                except (ValueError, TypeError) as e:
                    logger.warning(
                        "roa_expiry_parse_error",
                        prefix=roa.prefix,
                        not_after=roa.not_after,
                        error=str(e),
                    )

    @staticmethod
    def _parse_as_path(as_path_str: str) -> list[int]:
        """Parse a BGP AS path string into a list of ASNs.

        Handles AS_SET notation (e.g., "{1234,5678}") by flattening.
        Deduplicates consecutive prepends.
        """
        if not as_path_str:
            return []

        asns: list[int] = []
        for token in as_path_str.split():
            # Handle AS_SET: {1234,5678}
            token = token.strip("{}")
            for part in token.split(","):
                part = part.strip()
                if part.isdigit():
                    asn = int(part)
                    # Deduplicate consecutive prepends.
                    if not asns or asns[-1] != asn:
                        asns.append(asn)
        return asns
