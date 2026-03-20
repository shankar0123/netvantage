"""IP prefix utilities for BGP analysis.

Handles IPv4 and IPv6 prefix parsing, containment checks, and comparison.
Uses Python's ipaddress stdlib — no external dependencies.
"""

import ipaddress
from functools import lru_cache


def parse_prefix(prefix_str: str) -> ipaddress.IPv4Network | ipaddress.IPv6Network:
    """Parse a prefix string into an ip_network object.

    Accepts both IPv4 (e.g., "1.2.3.0/24") and IPv6 (e.g., "2001:db8::/32").
    Strict=False allows host bits to be set (e.g., "1.2.3.4/24" → "1.2.3.0/24").
    """
    return ipaddress.ip_network(prefix_str, strict=False)


@lru_cache(maxsize=4096)
def _cached_network(prefix_str: str) -> ipaddress.IPv4Network | ipaddress.IPv6Network:
    """Cached version of parse_prefix for hot-path lookups."""
    return parse_prefix(prefix_str)


def prefix_contains(parent: str, child: str) -> bool:
    """Check if parent prefix contains (covers) the child prefix.

    Examples:
        prefix_contains("10.0.0.0/8", "10.1.0.0/16")  → True
        prefix_contains("10.0.0.0/8", "10.0.0.0/8")    → True (equal)
        prefix_contains("10.1.0.0/16", "10.0.0.0/8")   → False
        prefix_contains("10.0.0.0/8", "192.168.0.0/16") → False
    """
    parent_net = _cached_network(parent)
    child_net = _cached_network(child)

    # Different address families can never contain each other.
    if parent_net.version != child_net.version:
        return False

    return (
        child_net.network_address >= parent_net.network_address
        and child_net.broadcast_address <= parent_net.broadcast_address
    )


def is_sub_prefix(monitored: str, announced: str) -> bool:
    """Check if announced is a more-specific (sub-prefix) of a monitored prefix.

    A sub-prefix is strictly more specific — same prefix doesn't count.

    Examples:
        is_sub_prefix("10.0.0.0/8", "10.1.0.0/16")  → True
        is_sub_prefix("10.0.0.0/8", "10.0.0.0/8")    → False (equal, not sub)
        is_sub_prefix("10.0.0.0/8", "192.168.0.0/16") → False
    """
    parent_net = _cached_network(monitored)
    child_net = _cached_network(announced)

    if parent_net.version != child_net.version:
        return False

    # Must be strictly more specific (longer prefix length).
    if child_net.prefixlen <= parent_net.prefixlen:
        return False

    return prefix_contains(monitored, announced)


def prefixes_overlap(a: str, b: str) -> bool:
    """Check if two prefixes overlap (one contains the other or they're equal)."""
    return prefix_contains(a, b) or prefix_contains(b, a)


def matches_any_monitored(announced: str, monitored_prefixes: set[str]) -> str | None:
    """Check if an announced prefix matches or is covered by any monitored prefix.

    Returns the matching monitored prefix, or None if no match.
    Checks both exact match and sub-prefix (announced is more specific).
    """
    for monitored in monitored_prefixes:
        if prefix_contains(monitored, announced):
            return monitored
    return None
