# BGP Monitoring Quick Start

Get BGP hijack detection, RPKI validation, and routing anomaly alerting running in under 10 minutes.

## Prerequisites

- Docker and Docker Compose (v2)
- A list of IP prefixes you want to monitor

## 1. Start the Infrastructure

```bash
task dev-up
```

This starts NATS, Prometheus, Grafana, PostgreSQL, Alertmanager, and Routinator. Routinator will begin syncing RPKI data from the five Regional Internet Registries — the initial sync takes a few minutes.

## 2. Create a BGP Analyzer Config

Create a file `bgp-analyzer-config.yaml`:

```yaml
# Prefixes to monitor (IPv4 and IPv6).
prefixes:
  - "203.0.113.0/24"
  - "2001:db8::/32"

# Expected origin ASN per prefix. Used for hijack detection.
# If an announcement comes from a different origin, it's flagged.
expected_origins:
  "203.0.113.0/24": 64500
  "2001:db8::/32": 64500

# BGP collectors to subscribe to.
collectors:
  - "routeviews"
  - "ris"

# Routinator URL (running in Docker Compose).
routinator_url: "http://localhost:8323"

# Prometheus metrics port.
metrics_port: 9100

# Alert if no BGP updates received within this many seconds.
staleness_threshold_seconds: 300

# ROA expiry warning thresholds (days).
roa_expiry_warning_days: [30, 14, 7, 1]

log_level: "info"
```

Replace the prefixes and expected origins with your own. Use your actual IP blocks and their authoritative origin ASN.

## 3. Run the BGP Analyzer

### Option A: Docker (recommended)

```bash
docker compose run --rm \
  -v $(pwd)/bgp-analyzer-config.yaml:/app/config.yaml \
  bgp-analyzer \
  python -m netvantage_bgp --config /app/config.yaml
```

### Option B: Local Python

```bash
cd bgp-analyzer
pip install -e ".[dev]"
python -m netvantage_bgp --config ../bgp-analyzer-config.yaml
```

You should see structured log output confirming the analyzer has started and connected to BGP streams.

## 4. Open the BGP Dashboard

Navigate to [http://localhost:3000](http://localhost:3000) (login: admin/admin) and open the **BGP Event Timeline** dashboard. You'll see:

- **Top row stats** — total events, hijacks detected, RPKI-invalid announcements, and withdrawals in the last hour
- **Event Timeline** — stacked bar chart of all BGP event types over time
- **Hijacks by Type** — origin hijacks, MOAS conflicts, and sub-prefix hijacks
- **Events by Prefix** — per-prefix event volume
- **ROA Expiry Countdown** — days until ROA expiration for each monitored prefix
- **RPKI Status Table** — current RPKI validation status per prefix/origin
- **Analyzer Staleness** — seconds since the last BGP update was received (alerts if >5 minutes)

Use the prefix and event_type dropdown filters at the top to focus on specific prefixes or event types.

## 5. Configure Alerts

Alert rules are pre-configured in `prometheus/rules/bgp_alerts.yml`:

| Alert | Severity | Fires When |
|---|---|---|
| NetVantageBGPHijackDetected | critical | Any hijack event detected |
| NetVantageBGPWithdrawal | warning | Withdrawal of a monitored prefix |
| NetVantageBGPAnalyzerStale | warning | No BGP updates received for 5+ minutes |
| NetVantageBGPRPKIInvalid | critical | RPKI-invalid announcement for monitored prefix |
| NetVantageBGPROAExpiringSoon | warning | ROA expires within 30 days |
| NetVantageBGPROAExpiryCritical | critical | ROA expires within 7 days |

To route alerts to Slack, edit `alertmanager/alertmanager.yml` and set your webhook URL:

```yaml
receivers:
  - name: "default"
    slack_configs:
      - api_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
        channel: "#network-alerts"
```

Then restart Alertmanager: `docker compose restart alertmanager`.

## What Gets Detected

**Prefix hijack** — Someone announces your prefix from an unauthorized origin AS. The analyzer compares every announcement's origin AS against your `expected_origins` config.

**MOAS conflict** — Multiple origin ASNs are simultaneously announcing the same prefix. This can indicate a hijack or a legitimate multi-homing setup.

**Sub-prefix hijack** — Someone announces a more-specific prefix (e.g., your /24 gets a rogue /25). More-specific routes win in BGP, making this a particularly dangerous attack.

**RPKI-invalid announcement** — An announcement for your prefix fails RPKI Route Origin Validation against the current ROA set in Routinator.

**ROA expiry** — Your ROAs are approaching expiration. Without valid ROAs, RPKI-enforcing networks may reject your legitimate announcements.

## Troubleshooting

**No events appearing?** Check that your prefixes are actually being announced in the global BGP table. Very small or unannounced prefixes won't generate events.

**Routinator not ready?** The initial RPKI sync takes 2-5 minutes. Check `docker compose logs routinator` for sync progress.

**Analyzer stuck?** Check `docker compose logs bgp-analyzer` for errors. Common issues: pybgpstream can't connect to collectors (firewall), or Routinator isn't reachable.
