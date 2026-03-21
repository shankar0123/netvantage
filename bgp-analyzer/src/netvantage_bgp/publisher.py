"""NATS publisher for BGP path correlation.

Publishes BGP path updates to NATS JetStream so the Go Metrics Processor
can correlate BGP-announced AS paths against traceroute-observed AS paths.

This is the bridge between the Python BGP Analyzer and the Go correlation
engine (M8: BGP + Traceroute Correlation).
"""

import json
import time

import structlog

logger = structlog.get_logger()


class BGPPathPublisher:
    """Publishes BGP path updates to NATS for correlation.

    If NATS is unavailable, the publisher silently drops messages and logs
    a warning. BGP analysis continues regardless — correlation is best-effort.
    """

    def __init__(self, nats_url: str = "nats://localhost:4222", subject: str = "netvantage.bgp.paths") -> None:
        self.nats_url = nats_url
        self.subject = subject
        self._nc = None
        self._js = None
        self._connected = False

    async def connect(self) -> None:
        """Connect to NATS. Non-blocking — called during analyzer startup."""
        try:
            import nats as nats_client

            self._nc = await nats_client.connect(self.nats_url)
            self._js = self._nc.jetstream()
            self._connected = True
            logger.info("nats_publisher_connected", url=self.nats_url, subject=self.subject)
        except Exception as e:
            self._connected = False
            logger.warning(
                "nats_publisher_connection_failed",
                url=self.nats_url,
                error=str(e),
                hint="BGP correlation will be unavailable until NATS is reachable",
            )

    async def publish_announcement(
        self,
        prefix: str,
        origin_asn: int,
        as_path: list[int],
        peer_asn: int,
        collector: str,
    ) -> None:
        """Publish a BGP announcement for correlation."""
        if not self._connected:
            return

        msg = {
            "prefix": prefix,
            "origin_asn": origin_asn,
            "as_path": as_path,
            "peer_asn": peer_asn,
            "collector": collector,
            "event_type": "announcement",
            "timestamp": time.time(),
        }

        try:
            await self._js.publish(self.subject, json.dumps(msg).encode())
        except Exception as e:
            logger.warning("nats_publish_failed", prefix=prefix, error=str(e))

    async def publish_withdrawal(self, prefix: str, peer_asn: int, collector: str) -> None:
        """Publish a BGP withdrawal for correlation."""
        if not self._connected:
            return

        msg = {
            "prefix": prefix,
            "peer_asn": peer_asn,
            "collector": collector,
            "event_type": "withdrawal",
            "timestamp": time.time(),
        }

        try:
            await self._js.publish(self.subject, json.dumps(msg).encode())
        except Exception as e:
            logger.warning("nats_publish_failed", prefix=prefix, error=str(e))

    async def close(self) -> None:
        """Close the NATS connection."""
        if self._nc and self._connected:
            await self._nc.close()
            self._connected = False
            logger.info("nats_publisher_closed")


class SyncBGPPathPublisher:
    """Synchronous wrapper around BGPPathPublisher for use in the threaded analyzer.

    Uses an internal event loop to bridge async NATS calls from sync code.
    """

    def __init__(self, nats_url: str = "nats://localhost:4222") -> None:
        self.nats_url = nats_url
        self._publisher = BGPPathPublisher(nats_url)
        self._loop = None
        self._connected = False

    def connect(self) -> None:
        """Connect to NATS (blocking)."""
        import asyncio

        try:
            self._loop = asyncio.new_event_loop()
            self._loop.run_until_complete(self._publisher.connect())
            self._connected = self._publisher._connected
        except Exception as e:
            logger.warning("sync_nats_connect_failed", error=str(e))
            self._connected = False

    def publish_announcement(
        self,
        prefix: str,
        origin_asn: int,
        as_path: list[int],
        peer_asn: int,
        collector: str,
    ) -> None:
        """Publish a BGP announcement (blocking)."""
        if not self._connected or not self._loop:
            return
        try:
            self._loop.run_until_complete(
                self._publisher.publish_announcement(prefix, origin_asn, as_path, peer_asn, collector)
            )
        except Exception as e:
            logger.warning("sync_nats_publish_failed", prefix=prefix, error=str(e))

    def publish_withdrawal(self, prefix: str, peer_asn: int, collector: str) -> None:
        """Publish a BGP withdrawal (blocking)."""
        if not self._connected or not self._loop:
            return
        try:
            self._loop.run_until_complete(
                self._publisher.publish_withdrawal(prefix, peer_asn, collector)
            )
        except Exception as e:
            logger.warning("sync_nats_publish_failed", prefix=prefix, error=str(e))

    def close(self) -> None:
        """Close NATS connection (blocking)."""
        if self._loop and self._connected:
            self._loop.run_until_complete(self._publisher.close())
            self._loop.close()
