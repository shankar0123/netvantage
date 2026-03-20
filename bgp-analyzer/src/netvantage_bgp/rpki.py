"""RPKI Route Origin Validation via Routinator HTTP API.

Queries a local Routinator instance to validate whether a BGP announcement's
origin AS is authorized by a Route Origin Authorization (ROA).

Routinator exposes:
  - /api/v1/validity/{asn}/{prefix} — per-announcement validation
  - /api/v1/export.json — full validated ROA set (for ROA lifecycle monitoring)
"""

from dataclasses import dataclass
from enum import Enum

import requests
import structlog

logger = structlog.get_logger()


class RPKIStatus(str, Enum):
    """RPKI validation status for a BGP announcement."""
    VALID = "valid"
    INVALID = "invalid"
    NOT_FOUND = "not-found"


@dataclass
class RPKIResult:
    """Result of RPKI validation for a single announcement."""
    prefix: str
    origin_asn: int
    status: RPKIStatus
    description: str = ""


@dataclass
class ROARecord:
    """A single Route Origin Authorization record."""
    prefix: str
    max_length: int
    asn: int
    not_after: str  # ISO 8601 expiry timestamp


class RPKIValidator:
    """Validates BGP announcements against RPKI via Routinator."""

    def __init__(self, routinator_url: str = "http://localhost:8323") -> None:
        self.base_url = routinator_url.rstrip("/")
        self._session = requests.Session()
        self._session.timeout = 5

    def validate(self, prefix: str, origin_asn: int) -> RPKIResult:
        """Validate a single BGP announcement against RPKI.

        Queries Routinator's /api/v1/validity endpoint.
        """
        url = f"{self.base_url}/api/v1/validity/AS{origin_asn}/{prefix}"
        try:
            resp = self._session.get(url)
            resp.raise_for_status()
            data = resp.json()

            validated = data.get("validated_route", {})
            validity = validated.get("validity", {})
            state = validity.get("state", "not-found")

            status = RPKIStatus(state) if state in RPKIStatus.__members__.values() else RPKIStatus.NOT_FOUND

            return RPKIResult(
                prefix=prefix,
                origin_asn=origin_asn,
                status=status,
                description=validity.get("description", ""),
            )
        except requests.RequestException as e:
            logger.warning("rpki_validation_failed", prefix=prefix, origin_asn=origin_asn, error=str(e))
            return RPKIResult(
                prefix=prefix,
                origin_asn=origin_asn,
                status=RPKIStatus.NOT_FOUND,
                description=f"Routinator query failed: {e}",
            )

    def get_roas_for_prefix(self, prefix: str) -> list[ROARecord]:
        """Fetch all ROAs covering a given prefix from Routinator's export.

        Used for ROA lifecycle monitoring (expiry, deletion, creation).
        """
        url = f"{self.base_url}/api/v1/export.json"
        try:
            resp = self._session.get(url)
            resp.raise_for_status()
            data = resp.json()

            roas = []
            for roa in data.get("roas", []):
                # Check if this ROA covers the target prefix.
                # TODO: Implement proper prefix containment check (not just string match).
                if roa.get("prefix") == prefix:
                    roas.append(ROARecord(
                        prefix=roa["prefix"],
                        max_length=roa.get("maxLength", 0),
                        asn=roa.get("asn", 0),
                        not_after=roa.get("notAfter", ""),
                    ))
            return roas
        except requests.RequestException as e:
            logger.warning("roa_fetch_failed", prefix=prefix, error=str(e))
            return []
