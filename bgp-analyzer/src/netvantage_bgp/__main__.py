"""Entry point for the BGP Analyzer service.

Usage:
    python -m netvantage_bgp --config config.yaml
    python -m netvantage_bgp  # uses defaults
"""

import argparse
import signal
import sys

import structlog
import yaml
from prometheus_client import start_http_server

from netvantage_bgp.analyzer import BGPAnalyzer
from netvantage_bgp.config import AnalyzerConfig

logger = structlog.get_logger()


def load_config(path: str) -> AnalyzerConfig:
    """Load analyzer config from a YAML file."""
    with open(path) as f:
        raw = yaml.safe_load(f)

    if raw is None:
        return AnalyzerConfig()

    return AnalyzerConfig(
        prefixes=raw.get("prefixes", []),
        expected_origins=raw.get("expected_origins", {}),
        collectors=raw.get("collectors", ["routeviews", "ris"]),
        routinator_url=raw.get("routinator_url", "http://localhost:8323"),
        metrics_port=raw.get("metrics_port", 9100),
        staleness_threshold_seconds=raw.get("staleness_threshold_seconds", 300),
        roa_expiry_warning_days=raw.get("roa_expiry_warning_days", [30, 14, 7, 1]),
        log_level=raw.get("log_level", "info"),
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="NetVantage BGP Analyzer")
    parser.add_argument("--config", help="Path to YAML config file")
    parser.add_argument(
        "--metrics-port",
        type=int,
        default=None,
        help="Prometheus metrics HTTP port (overrides config file)",
    )
    args = parser.parse_args()

    # Load config from file or use defaults.
    if args.config:
        try:
            config = load_config(args.config)
            logger.info("config_loaded", path=args.config)
        except Exception as e:
            logger.error("config_load_failed", path=args.config, error=str(e))
            return 1
    else:
        config = AnalyzerConfig()

    # CLI overrides.
    if args.metrics_port is not None:
        config.metrics_port = args.metrics_port

    # Configure structured logging.
    structlog.configure(
        wrapper_class=structlog.make_filtering_bound_logger(
            structlog.get_level_from_name(config.log_level)
        ),
    )

    logger.info(
        "starting_bgp_analyzer",
        version="0.1.0",
        metrics_port=config.metrics_port,
        prefix_count=len(config.prefixes),
    )

    if not config.prefixes:
        logger.warning(
            "no_prefixes_configured",
            hint="Add prefixes to your config file or pass --config with monitored prefixes",
        )

    # Start Prometheus metrics HTTP server.
    start_http_server(config.metrics_port)
    logger.info("prometheus_metrics_server_started", port=config.metrics_port)

    # Create and run the analyzer.
    analyzer = BGPAnalyzer(config)

    # Wire up signal handlers for graceful shutdown.
    def handle_signal(signum, frame):
        logger.info("signal_received", signal=signal.Signals(signum).name)
        analyzer.stop()

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    try:
        analyzer.run()
    except KeyboardInterrupt:
        analyzer.stop()
    except Exception as e:
        logger.error("bgp_analyzer_fatal_error", error=str(e), exc_info=True)
        return 1

    logger.info("bgp_analyzer_stopped")
    return 0


if __name__ == "__main__":
    sys.exit(main())
