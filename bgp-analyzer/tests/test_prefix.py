"""Tests for prefix utility module."""

import pytest

from netvantage_bgp.prefix import (
    parse_prefix,
    prefix_contains,
    is_sub_prefix,
    prefixes_overlap,
    matches_any_monitored,
)


class TestParsePrefix:
    def test_ipv4(self):
        net = parse_prefix("10.0.0.0/8")
        assert str(net) == "10.0.0.0/8"

    def test_ipv4_host_bits_set(self):
        net = parse_prefix("10.1.2.3/8")
        assert str(net) == "10.0.0.0/8"

    def test_ipv6(self):
        net = parse_prefix("2001:db8::/32")
        assert str(net) == "2001:db8::/32"

    def test_invalid_raises(self):
        with pytest.raises(ValueError):
            parse_prefix("not-a-prefix")


class TestPrefixContains:
    def test_parent_contains_child(self):
        assert prefix_contains("10.0.0.0/8", "10.1.0.0/16") is True

    def test_exact_match(self):
        assert prefix_contains("10.0.0.0/8", "10.0.0.0/8") is True

    def test_child_does_not_contain_parent(self):
        assert prefix_contains("10.1.0.0/16", "10.0.0.0/8") is False

    def test_no_overlap(self):
        assert prefix_contains("10.0.0.0/8", "192.168.0.0/16") is False

    def test_different_families(self):
        assert prefix_contains("10.0.0.0/8", "2001:db8::/32") is False

    def test_ipv6_containment(self):
        assert prefix_contains("2001:db8::/32", "2001:db8:1::/48") is True


class TestIsSubPrefix:
    def test_more_specific(self):
        assert is_sub_prefix("10.0.0.0/8", "10.1.0.0/16") is True

    def test_exact_match_is_not_sub(self):
        assert is_sub_prefix("10.0.0.0/8", "10.0.0.0/8") is False

    def test_less_specific_is_not_sub(self):
        assert is_sub_prefix("10.1.0.0/16", "10.0.0.0/8") is False

    def test_unrelated(self):
        assert is_sub_prefix("10.0.0.0/8", "192.168.0.0/16") is False

    def test_ipv6_sub_prefix(self):
        assert is_sub_prefix("2001:db8::/32", "2001:db8:1::/48") is True


class TestPrefixesOverlap:
    def test_overlap(self):
        assert prefixes_overlap("10.0.0.0/8", "10.1.0.0/16") is True

    def test_no_overlap(self):
        assert prefixes_overlap("10.0.0.0/8", "192.168.0.0/16") is False


class TestMatchesAnyMonitored:
    def test_exact_match(self):
        monitored = {"10.0.0.0/8", "192.168.0.0/16"}
        assert matches_any_monitored("10.0.0.0/8", monitored) == "10.0.0.0/8"

    def test_sub_prefix_match(self):
        monitored = {"10.0.0.0/8"}
        assert matches_any_monitored("10.1.0.0/16", monitored) == "10.0.0.0/8"

    def test_no_match(self):
        monitored = {"10.0.0.0/8"}
        assert matches_any_monitored("192.168.0.0/16", monitored) is None

    def test_empty_monitored(self):
        assert matches_any_monitored("10.0.0.0/8", set()) is None
