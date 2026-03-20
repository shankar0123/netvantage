# Guided Demo: BGP Monitoring

This walkthrough sets up BGP hijack detection, RPKI validation, and routing anomaly alerting from scratch. Each step explains what's happening and why, so you understand the system — not just the commands.

**New to BGP?** Read [Understanding NetVantage](concepts.md) first. It explains BGP, hijacks, RPKI, and why this monitoring matters — no networking background required.

**Haven't started the dev stack?** Run through the [Guided Demo](quickstart.md) first. You'll need the infrastructure services (Prometheus, Grafana, Routinator, etc.) running.

## What You'll Build

By the end of this demo, you'll have a running BGP analyzer that watches your IP prefixes across the global routing table and alerts you when something suspicious happens. Specifically:

- **Hijack detection** — Someone announces your prefix from an unauthorized network
- **MOAS detection** — Multiple networks simultaneously claim your prefix
- **Sub-prefix hijack detection** — Someone announces a more-specific route to steal your traffic
- **RPKI validation** — Every announcement is checked against the cryptographic ROA database
- **ROA expiry monitoring** — Warnings before your ROAs expire and leave you unprotected

All of this feeds into a pre-built Grafana dashboard with 10 panels and six pre-configured alert rules.

## Step 1: Understand What You're Configuring

The BGP analyzer needs to know three things:

**Which prefixes are yours.** These are the IP blocks you want to monitor. The analyzer will filter the global BGP stream and only process events involving these prefixes (or more-specific sub-prefixes of them).

**Who should be announcing them.** For each prefix, you specify the expected origin AS (Autonomous System number) — the network authorized to announce it. Any announcement from a different origin triggers a hijack alert.

**Where to look.** BGP data comes from collector projects that peer with hundreds of networks worldwide. RouteViews (University of Oregon) and RIPE RIS (RIPE NCC) are the two major public projects. Between them, they observe BGP announcements from ~1,000 vantage points across the internet.

## Step 2: Create Your Config File

Create `bgp-analyzer-config.yaml` in the project root:

```yaml
# Prefixes to monitor (IPv4 and IPv6).
# Replace these with your actual IP blocks.
prefixes:
  - "203.0.113.0/24"
  - "2001:db8::/32"

# Expected origin ASN for each prefix.
# This is YOUR AS number — the one authorized to announce these prefixes.
# Find yours at https://bgp.he.net/ or ask your network team.
expected_origins:
  "203.0.113.0/24": 64500
  "2001:db8::/32": 64500

# BGP collectors to subscribe to.
# "routeviews" = RouteViews project (University of Oregon)
# "ris" = RIPE Routing Information Service
# Using both gives the widest visibility into the global routing table.
collectors:
  - "routeviews"
  - "ris"

# Routinator HTTP API for RPKI validation.
# This matches the Docker Compose setup — Routinator runs on port 8323.
routinator_url: "http://localhost:8323"

# Prometheus metrics endpoint port.
metrics_port: 9100

# Staleness detection: alert if the analyzer stops receiving BGP updates.
# 300 seconds (5 minutes) means: if we go 5 minutes without a single
# BGP update, something is wrong (network issue, collector down, etc.)
staleness_threshold_seconds: 300

# ROA expiry warnings fire at these thresholds.
# Why multiple thresholds? A 30-day warning gives you time to plan.
# A 1-day warning means "act now or your routes may be rejected."
roa_expiry_warning_days: [30, 14, 7, 1]

log_level: "info"
```

**How to find your AS number:** If you manage your own IP space, your network team knows your ASN. If you're evaluating NetVantage, you can use any public prefix/ASN pair for testing — try a well-known service's prefix and expected origin.

**Why both collectors?** RouteViews and RIPE RIS have different peering arrangements. An event visible from RouteViews might not appear in RIPE RIS and vice versa. Using both maximizes the probability of detecting anomalies.

## Step 3: Verify Routinator Is Ready

Before starting the analyzer, make sure Routinator has finished its initial RPKI sync:

```bash
docker compose logs routinator | tail -20
```

**What to look for:** Messages about successful RSYNC/RRDP fetches from each of the five RIRs (AFRINIC, APNIC, ARIN, LACNIC, RIPE NCC). The initial sync downloads every published ROA worldwide — typically 400,000+ records. This takes 2-5 minutes on first run, then seconds for incremental updates.

**Why this matters:** If you start the BGP analyzer before Routinator is ready, RPKI validation will return "not-found" for everything (because the ROA database is empty). The analyzer handles this gracefully — it won't crash — but your RPKI status data won't be meaningful until Routinator catches up.

You can also verify Routinator directly: [http://localhost:8323/api/v1/status](http://localhost:8323/api/v1/status) should show the number of valid ROAs.

## Step 4: Start the BGP Analyzer

### Option A: Docker (recommended)

```bash
docker compose run --rm \
  -v $(pwd)/bgp-analyzer-config.yaml:/app/config.yaml \
  bgp-analyzer \
  python -m netvantage_bgp --config /app/config.yaml
```

**Why Docker?** The BGP analyzer depends on `pybgpstream`, which has C library dependencies (`libparsebgp`). The Docker image comes with everything pre-built. Running locally requires installing these C libraries yourself.

### Option B: Local Python (for development)

```bash
cd bgp-analyzer
pip install -e ".[stream,dev]"
python -m netvantage_bgp --config ../bgp-analyzer-config.yaml
```

**Note:** `.[stream]` installs pybgpstream, which requires `libparsebgp-dev` and `libwandio1-dev` on your system. On macOS: `brew install libparsebgp`. On Ubuntu: `apt-get install libparsebgp-dev libwandio1-dev`.

**What you should see:** Structured log output showing the analyzer has initialized, connected to BGP collectors, and started the ROA monitor thread. If your monitored prefixes are actively announced in the global table, you'll start seeing `bgp_announcement` log events within seconds.

## Step 5: Explore the BGP Dashboard

Open Grafana at [http://localhost:3000](http://localhost:3000) and navigate to the **BGP Event Timeline** dashboard.

### Understanding Each Panel

**Top row: Summary Stats** — Four stat panels showing event counts over the last hour. These give you an at-a-glance health check. A nonzero hijack count demands immediate attention. RPKI-invalid counts might be normal (someone somewhere is always misconfigured) or might indicate a targeted attack on your prefix.

**Event Timeline** — A stacked bar chart showing event volume over time, broken down by type (announcement, withdrawal, origin_change, path_change, hijack, rpki_invalid). This is the primary panel for spotting anomalies — a sudden spike in any event type warrants investigation. The time axis lets you correlate BGP events with incidents your users report.

**Hijacks by Type** — Breaks hijack events into categories: `origin` (someone else announced your prefix), `moas` (multiple origins for same prefix), `sub_prefix` (someone announced a more-specific route). Sub-prefix hijacks are the most dangerous because they silently steal traffic without triggering traditional monitoring.

**Events by Prefix** — Shows which of your monitored prefixes are generating the most events. A prefix with unusually high activity might be under attack or experiencing instability in its upstream path.

**ROA Expiry Countdown** — Color-coded countdown for each monitored prefix's ROA. Green (>30 days), yellow (14-30 days), orange (7-14 days), red (<7 days). An expired ROA means RPKI-enforcing networks might reject your legitimate announcements. This panel is your early warning system.

**RPKI Status Table** — Shows the current validation status of each prefix/origin combination: VALID (announcement matches a ROA), INVALID (announcement contradicts a ROA — this is bad), or NOT FOUND (no ROA exists for this prefix). If you see your own prefix as INVALID, either your ROA is wrong or someone is hijacking you.

**Analyzer Staleness** — Seconds since the last BGP update was received, per collector. If this exceeds the staleness threshold (default: 5 minutes), the analyzer might have lost its connection to BGP collectors. An alert fires automatically.

### Using the Filters

The dashboard has two template variables at the top: **prefix** and **event_type**. Use these to focus on a specific prefix or event category. Multi-select is supported — you can compare two prefixes side by side.

## Step 6: Understand the Alerts

Six alert rules are pre-configured in `prometheus/rules/bgp_alerts.yml`. Here's what each one detects and why it matters:

| Alert | Severity | What It Means | Why You Care |
|---|---|---|---|
| **NetVantageBGPHijackDetected** | Critical | Any hijack event fired in the last 5 minutes | Someone may be announcing your IP space without authorization. Investigate immediately. |
| **NetVantageBGPWithdrawal** | Warning | A monitored prefix was withdrawn by a peer | Could be a peer going down (normal) or the start of a depeering event (not normal if unexpected). |
| **NetVantageBGPAnalyzerStale** | Warning | No BGP updates received for 5+ minutes | The analyzer may have lost its connection. BGP data is flowing constantly — silence means something is broken. |
| **NetVantageBGPRPKIInvalid** | Critical | An announcement for your prefix failed RPKI validation | Either someone is announcing your prefix without authorization, or your ROA is misconfigured. Both need immediate action. |
| **NetVantageBGPROAExpiringSoon** | Warning | A ROA expires within 30 days | Time to renew. Contact your RIR or RPKI provider. |
| **NetVantageBGPROAExpiryCritical** | Critical | A ROA expires within 7 days | Urgent. If the ROA expires, RPKI-enforcing networks may drop your legitimate traffic. |

### Routing Alerts to Slack

Edit `alertmanager/alertmanager.yml`:

```yaml
receivers:
  - name: "default"
    slack_configs:
      - api_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
        channel: "#network-alerts"
        title: "{{ .GroupLabels.alertname }}"
        text: "{{ range .Alerts }}{{ .Annotations.description }}\n{{ end }}"
```

Then restart: `docker compose restart alertmanager`.

**Why Alertmanager routes instead of Prometheus sending directly:** Prometheus evaluates rules and decides "this condition is true." But you don't want 50 alerts for the same hijack. Alertmanager groups related alerts, deduplicates, throttles, and routes to the right channel. Critical BGP alerts go to the on-call channel; ROA expiry warnings go to the planning channel. This routing logic stays separate from detection logic.

## What You've Learned

You've set up a live BGP monitoring system that watches the global routing table for threats to your IP space. You understand what each detection type catches, why RPKI validation matters, and how alerts flow from detection to notification.

The BGP analyzer runs independently of the Go agent pipeline — it just needs Prometheus, Grafana, and Routinator from the Docker Compose stack. This is intentional: it means BGP monitoring works from day one, before any synthetic probes are deployed.

## Troubleshooting

**No events appearing?** Your prefixes need to be actively announced in the global BGP table. Private IP space (10.0.0.0/8, 192.168.0.0/16) and documentation prefixes (203.0.113.0/24) won't appear in public BGP feeds. For testing, use a real publicly-announced prefix.

**Everything shows RPKI NOT FOUND?** Either Routinator hasn't finished syncing (check logs), or the prefix has no ROA published. Many prefixes don't have ROAs yet — RPKI adoption is growing but not universal.

**Analyzer exits immediately?** Check that pybgpstream is installed (Docker handles this) and that your machine has outbound connectivity to BGP collectors (TCP port 8001 for RouteViews stream, port 8282 for RIS Live).

## Next Steps

**[Architecture Deep Dive](ARCHITECTURE.md)** — Understand the transport abstraction, agent resilience patterns, and why each component is built the way it is.

**[Contributing](CONTRIBUTING.md)** — How the codebase is organized, development conventions, and how to add new monitoring capabilities.
