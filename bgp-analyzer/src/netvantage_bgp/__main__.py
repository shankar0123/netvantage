"""Entry point for the BGP Analyzer service.

Usage:
    python -m netvantage_bgp --config config.yaml
    python -m netvantage_bgp  # uses defaults
"""

import argparse
import sys

import structlog
from prometheus_client import start_http_server

from netvantage_bgp.analyzer import BGPAnalyzer
from netvantage_bgp.config import AnalyzerConfig

logger = structlog.get_logger()


def main() -> int:
    parser = argparse.ArgumentParser(description="NetVantage BGP Analyzer")
    parser.add_argument("--config", help="Path to YAML config file")
    parser.add_argument("--metrics-port", type=int, default=9100, help="Prometheus metrics HTTP port")
    args = parser.parse_args()

    # TODO: Load config from YAML file if --config is provided.
    config = AnalyzerConfig(metrics_port=args.metrics_port)

    structlog.configure(
        wrapper_class=structlog.make_filtering_bound_logger(
            structlog.get_level_from_name(config.log_level)
        ),
    )

    logger.info("starting_bgp_analyzer", version="0.1.0", metrics_port=config.metrics_port)

    # Start Prometheus metrics HTTP server.
    start_http_server(config.metrics_port)
    logger.info("prometheus_metrics_server_started", port=config.metrics_port)

    # Run the analyzer (blocks until stopped).
    analyzer = BGPAnalyzer(config)
    try:
        analyzer.run()
    except KeyboardInterrupt:
        logger.info("bgp_analyzer_stopped")
    except Exception as e:
        logger.error("bgp_analyzer_fatal_error", error=str(e))
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
