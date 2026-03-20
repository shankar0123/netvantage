"""Tests for RPKI validation module."""

from unittest.mock import MagicMock, patch

from netvantage_bgp.rpki import RPKIStatus, RPKIValidator


class TestRPKIValidator:
    """Tests for RPKIValidator against mock Routinator responses."""

    def test_valid_announcement(self):
        """RPKI-valid announcement should return VALID status."""
        validator = RPKIValidator("http://localhost:8323")

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "validated_route": {
                "validity": {
                    "state": "valid",
                    "description": "Matches ROA",
                }
            }
        }
        mock_response.raise_for_status = MagicMock()

        with patch.object(validator._session, "get", return_value=mock_response):
            result = validator.validate("1.2.3.0/24", 64500)

        assert result.status == RPKIStatus.VALID
        assert result.prefix == "1.2.3.0/24"
        assert result.origin_asn == 64500

    def test_invalid_announcement(self):
        """RPKI-invalid announcement should return INVALID status."""
        validator = RPKIValidator("http://localhost:8323")

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "validated_route": {
                "validity": {
                    "state": "invalid",
                    "description": "Origin AS not authorized",
                }
            }
        }
        mock_response.raise_for_status = MagicMock()

        with patch.object(validator._session, "get", return_value=mock_response):
            result = validator.validate("1.2.3.0/24", 99999)

        assert result.status == RPKIStatus.INVALID

    def test_not_found(self):
        """Prefix with no ROA should return NOT_FOUND status."""
        validator = RPKIValidator("http://localhost:8323")

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "validated_route": {
                "validity": {
                    "state": "not-found",
                    "description": "No ROA found",
                }
            }
        }
        mock_response.raise_for_status = MagicMock()

        with patch.object(validator._session, "get", return_value=mock_response):
            result = validator.validate("10.0.0.0/8", 64500)

        assert result.status == RPKIStatus.NOT_FOUND

    def test_routinator_unreachable(self):
        """When Routinator is down, validation should return NOT_FOUND gracefully."""
        validator = RPKIValidator("http://localhost:8323")

        import requests
        with patch.object(validator._session, "get", side_effect=requests.ConnectionError("refused")):
            result = validator.validate("1.2.3.0/24", 64500)

        assert result.status == RPKIStatus.NOT_FOUND
        assert "failed" in result.description.lower()
