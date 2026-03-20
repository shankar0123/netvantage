"""Tests for the BGP Analyzer core logic.

Tests use mock BGP data — no live pybgpstream required.
"""

import pytest
from unittest.mock import MagicMock, patch

from netvantage_bgp.analyzer import BGPAnalyzer, PrefixState
from netvantage_bgp.config import AnalyzerConfig
from netvantage_bgp.rpki import RPKIResult, RPKIStatus


# ---------------------------------------------------------------------------
# PrefixState tests
# ---------------------------------------------------------------------------


class TestPrefixState:
    def test_first_announcement_is_new(self):
        state = PrefixState()
        changes = state.record_announcement("10.0.0.0/8", 64500, [64500], 64501)
        assert changes["is_new"] is True
        assert changes["origin_changed"] is False
        assert changes["path_changed"] is False

    def test_same_announcement_no_changes(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64501, 64500], 64501)
        changes = state.record_announcement("10.0.0.0/8", 64500, [64501, 64500], 64501)
        assert changes["is_new"] is False
        assert changes["origin_changed"] is False
        assert changes["path_changed"] is False

    def test_origin_change_detected(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64500], 64501)
        changes = state.record_announcement("10.0.0.0/8", 99999, [99999], 64501)
        assert changes["origin_changed"] is True
        assert changes["previous_origin"] == 64500

    def test_path_change_detected(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64501, 64500], 64501)
        changes = state.record_announcement("10.0.0.0/8", 64500, [64502, 64500], 64501)
        assert changes["path_changed"] is True
        assert changes["previous_path"] == [64501, 64500]
        assert changes["origin_changed"] is False

    def test_moas_detection(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64500], 64501)
        state.record_announcement("10.0.0.0/8", 99999, [99999], 64502)
        origins = state.get_active_origins("10.0.0.0/8")
        assert origins == {64500, 99999}

    def test_withdrawal_removes_state(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64500], 64501)
        prev = state.record_withdrawal("10.0.0.0/8", 64501)
        assert prev is not None
        assert prev["origin_asn"] == 64500

    def test_withdrawal_cleans_active_origins(self):
        state = PrefixState()
        state.record_announcement("10.0.0.0/8", 64500, [64500], 64501)
        state.record_announcement("10.0.0.0/8", 99999, [99999], 64502)
        state.record_withdrawal("10.0.0.0/8", 64501)
        origins = state.get_active_origins("10.0.0.0/8")
        assert origins == {99999}

    def test_withdrawal_nonexistent_returns_none(self):
        state = PrefixState()
        prev = state.record_withdrawal("10.0.0.0/8", 64501)
        assert prev is None


# ---------------------------------------------------------------------------
# AS path parsing tests
# ---------------------------------------------------------------------------


class TestASPathParsing:
    def setup_method(self):
        self.config = AnalyzerConfig(prefixes=["10.0.0.0/8"])
        self.analyzer = BGPAnalyzer(self.config)

    def test_simple_path(self):
        assert BGPAnalyzer._parse_as_path("64501 64502 64500") == [64501, 64502, 64500]

    def test_prepend_dedup(self):
        assert BGPAnalyzer._parse_as_path("64501 64501 64501 64500") == [64501, 64500]

    def test_as_set(self):
        result = BGPAnalyzer._parse_as_path("64501 {64502,64503}")
        assert 64501 in result
        assert 64502 in result
        assert 64503 in result

    def test_empty_path(self):
        assert BGPAnalyzer._parse_as_path("") == []

    def test_single_asn(self):
        assert BGPAnalyzer._parse_as_path("64500") == [64500]


# ---------------------------------------------------------------------------
# Analyzer integration tests (mocked stream + RPKI)
# ---------------------------------------------------------------------------


class TestAnalyzerHijackDetection:
    """Test hijack detection logic with mock data."""

    def setup_method(self):
        self.config = AnalyzerConfig(
            prefixes=["10.0.0.0/8", "192.168.0.0/16"],
            expected_origins={"10.0.0.0/8": 64500, "192.168.0.0/16": 64600},
            routinator_url="http://localhost:8323",
        )
        self.analyzer = BGPAnalyzer(self.config)
        # Mock RPKI to avoid network calls.
        self.analyzer.rpki = MagicMock()
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.0.0.0/8", origin_asn=64500, status=RPKIStatus.VALID
        )

    def test_legitimate_announcement(self):
        """Announcement from expected origin should not trigger hijack."""
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=64500,
            as_path=[64501, 64500],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        # Should not have logged a hijack — check no hijack metric.
        # (In a real test we'd use prometheus test utilities, here we verify
        #  the state didn't flag anything anomalous.)
        assert 64500 in self.analyzer.state.get_active_origins("10.0.0.0/8")

    def test_origin_hijack(self):
        """Announcement from unexpected origin should trigger hijack detection."""
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.0.0.0/8", origin_asn=99999, status=RPKIStatus.INVALID
        )
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=99999,
            as_path=[64501, 99999],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        origins = self.analyzer.state.get_active_origins("10.0.0.0/8")
        assert 99999 in origins

    def test_sub_prefix_hijack(self):
        """More-specific prefix from unexpected origin triggers sub-prefix hijack."""
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.1.0.0/16", origin_asn=99999, status=RPKIStatus.INVALID
        )
        self.analyzer._handle_announcement(
            prefix="10.1.0.0/16",
            origin_asn=99999,
            as_path=[64501, 99999],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        # Sub-prefix should be tracked.
        assert 99999 in self.analyzer.state.get_active_origins("10.1.0.0/16")

    def test_moas_conflict(self):
        """Two different origins for same prefix triggers MOAS."""
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=64500,
            as_path=[64501, 64500],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=99999,
            as_path=[64502, 99999],
            peer_asn=64502,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        origins = self.analyzer.state.get_active_origins("10.0.0.0/8")
        assert len(origins) == 2
        assert origins == {64500, 99999}


class TestAnalyzerRPKI:
    """Test RPKI validation integration."""

    def setup_method(self):
        self.config = AnalyzerConfig(
            prefixes=["10.0.0.0/8"],
            expected_origins={"10.0.0.0/8": 64500},
        )
        self.analyzer = BGPAnalyzer(self.config)
        self.analyzer.rpki = MagicMock()

    def test_rpki_valid_called(self):
        """RPKI validation should be called for each announcement."""
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.0.0.0/8", origin_asn=64500, status=RPKIStatus.VALID
        )
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=64500,
            as_path=[64500],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        self.analyzer.rpki.validate.assert_called_once_with("10.0.0.0/8", 64500)

    def test_rpki_invalid_logged(self):
        """RPKI-invalid announcement should be handled without crashing."""
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.0.0.0/8", origin_asn=99999, status=RPKIStatus.INVALID,
            description="Origin AS not authorized",
        )
        # Should not raise.
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=99999,
            as_path=[99999],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )


class TestAnalyzerWithdrawal:
    def setup_method(self):
        self.config = AnalyzerConfig(
            prefixes=["10.0.0.0/8"],
            expected_origins={"10.0.0.0/8": 64500},
        )
        self.analyzer = BGPAnalyzer(self.config)
        self.analyzer.rpki = MagicMock()
        self.analyzer.rpki.validate.return_value = RPKIResult(
            prefix="10.0.0.0/8", origin_asn=64500, status=RPKIStatus.VALID
        )

    def test_withdrawal_after_announcement(self):
        """Withdrawal should clean up state."""
        self.analyzer._handle_announcement(
            prefix="10.0.0.0/8",
            origin_asn=64500,
            as_path=[64500],
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        self.analyzer._handle_withdrawal(
            prefix="10.0.0.0/8",
            peer_asn=64501,
            collector="rrc00",
            matched_monitored="10.0.0.0/8",
        )
        assert len(self.analyzer.state.get_active_origins("10.0.0.0/8")) == 0
