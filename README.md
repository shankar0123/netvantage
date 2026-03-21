# NetVantage — Distributed Network Monitoring & BGP Analysis

A source-available alternative to Cisco ThousandEyes. Deploy lightweight agents across your infrastructure, monitor reachability and performance from every vantage point, and correlate what you observe with what BGP says should be happening — with dashboards, alerts, and RPKI validation out of the box.

[![license](https://img.shields.io/badge/license-BSL_1.1-green)](LICENSE)
[![go](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![python](https://img.shields.io/badge/python-3.12+-3776AB?logo=python&logoColor=white)](https://python.org)
[![status](https://img.shields.io/badge/status-active_development-brightgreen)](docs/ROADMAP.md)

## What It Does

NetVantage gives you a single platform for synthetic monitoring *and* BGP routing analysis. The **canary agents** are single Go binaries (sub-10MB) that run at your Points of Presence and execute synthetic tests — ICMP ping, DNS resolution, HTTP/S with full timing breakdown, traceroute with per-hop ASN and geolocation enrichment. Results flow through NATS JetStream into Prometheus and Grafana dashboards that are provisioned as code and versioned in this repo.

The **BGP analyzer** is an independent Python service that monitors the global routing table via RouteViews and RIPE RIS. It detects prefix hijacks, MOAS conflicts, sub-prefix hijacks, unexpected withdrawals, and AS path anomalies for your IP space. Every announcement is validated against RPKI ROAs through Routinator, with immediate alerts on invalid origins and countdown warnings before your ROAs expire.

The **correlation engine** (M8) is what ties both systems together: it compares the AS path BGP announces against the AS path traceroute actually observes. Discrepancies reveal route leaks, traffic engineering problems, and hijacks that neither system catches alone. This is ThousandEyes-grade capability in a source-available package.

```mermaid
graph TB
    subgraph POPs
        A1["Agent (POP 1)"]
        A2["Agent (POP 2)"]
    end

    subgraph Hub
        NATS[NATS JetStream]
        MP[Metrics Processor]
        PROM[Prometheus]
        GRAF[Grafana Dashboards]
        ROUT[Routinator — RPKI]
        CP[Control Plane API]
        PG[PostgreSQL]
    end

    BGP["BGP Analyzer (Python)"]

    A1 -->|publish results| NATS
    A2 -->|publish results| NATS
    NATS -->|consume| MP
    MP -->|remote_write| PROM
    BGP -->|write metrics| PROM
    BGP -->|validate ROAs| ROUT
    PROM --> GRAF
    CP --> PG
    CP -->|config sync| A1
    CP -->|config sync| A2
```

Agents communicate through a transport abstraction layer (`Publisher`/`Consumer` interfaces), defaulting to NATS JetStream for development and small deployments. Kafka is available as a production backend for 50+ POP deployments — the swap requires a single config change, no code changes.

## Quick Start

```bash
git clone https://github.com/shankar0123/netvantage.git
cd netvantage

# Start all infrastructure
task dev-up

# Build Go services (go mod tidy on first clone)
go mod tidy
task build-agent && task build-server && task build-processor
```

| Service | URL | Credentials |
|---|---|---|
| Grafana | http://localhost:3000 | admin / netvantage |
| Prometheus | http://localhost:9090 | — |
| NATS Monitoring | http://localhost:8222 | — |
| Alertmanager | http://localhost:9093 | — |
| Control Plane API | http://localhost:8080 | API key (Bearer token) |

See the [Guided Demo](docs/quickstart.md) for a full walkthrough with explanations at every step.

## Control Plane API

The control plane is a Go REST API (`net/http` + chi router) backed by PostgreSQL. All `/api/v1/` endpoints require a Bearer token (API key). The server runs on `:8080` by default, configurable via `NETVANTAGE_SERVER_ADDR`.

### Authentication

Every request to `/api/v1/*` must include an `Authorization: Bearer <api-key>` header. Keys are SHA-256 hashed at rest and support role-based access (`admin`, `agent`) with optional expiry. The healthcheck at `/healthz` is unauthenticated.

### Agents

Agents self-register with the control plane, send periodic heartbeats, and pull their test assignments via config sync.

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/agents` | Register a new agent. Body: `{"id", "pop_name", "version", "capabilities"}` |
| `GET` | `/api/v1/agents` | List all registered agents |
| `GET` | `/api/v1/agents/{id}` | Get a single agent by ID |
| `DELETE` | `/api/v1/agents/{id}` | Deregister an agent |
| `POST` | `/api/v1/agents/{id}/heartbeat` | Agent liveness signal. Body: `{"version", "status", "active_tests"}` |
| `GET` | `/api/v1/agents/{id}/config?pop=<name>` | Config sync — returns enabled tests assigned to the agent's POP |

### Points of Presence (POPs)

POPs are logical locations where agents run. Every agent must reference an existing POP at registration time.

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/pops` | Create a POP. Body: `{"name", "provider", "city", "country", "latitude", "longitude"}` |
| `GET` | `/api/v1/pops` | List all POPs |
| `GET` | `/api/v1/pops/{name}` | Get a single POP |
| `DELETE` | `/api/v1/pops/{name}` | Remove a POP |

### Test Definitions

Test definitions describe *what* to monitor. They are created centrally and assigned to POPs — agents pull their assignments via config sync.

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/tests` | Create a test. Body: `{"id", "name", "test_type", "target", "interval_ms", "timeout_ms", "config", "pops"}` |
| `GET` | `/api/v1/tests` | List all test definitions |
| `GET` | `/api/v1/tests/{id}` | Get a single test definition |
| `PUT` | `/api/v1/tests/{id}` | Update a test (partial update — only send fields you want to change) |
| `DELETE` | `/api/v1/tests/{id}` | Delete a test definition (cascades to assignments) |
| `POST` | `/api/v1/tests/{id}/assign` | Assign a test to POPs. Body: `{"pops": ["us-east-1-aws", "eu-west-1-aws"]}` |

Valid `test_type` values: `ping`, `dns`, `http`, `traceroute`. Omitting `pops` on creation assigns the test globally (runs on all POPs).

### Rate Limiting

All endpoints are rate-limited per IP at 100 requests/second with a burst of 200. Exceeding the limit returns `429 Too Many Requests` with a `Retry-After` header.

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `NETVANTAGE_SERVER_ADDR` | `:8080` | Server listen address |
| `NETVANTAGE_DB_URL` | `postgres://netvantage:netvantage-dev@localhost:5432/netvantage?sslmode=disable` | PostgreSQL connection string |
| `NETVANTAGE_JWT_SECRET` | `dev-secret-change-in-production` | Secret for future JWT signing |

## Canary Types

Each canary type is compiled into the agent binary and implements the `Canary` interface. Results are published to NATS JetStream, consumed by the Metrics Processor, and written to Prometheus.

### Ping (ICMP)

Measures round-trip time, packet loss, and jitter to any IP or hostname. Uses [pro-bing](https://github.com/prometheus-community/pro-bing) for ICMP — supports both privileged raw sockets and unprivileged UDP mode.

**Prometheus metrics:** `netvantage_ping_rtt_seconds{target, pop}`, `netvantage_ping_packet_loss_ratio`, `netvantage_ping_jitter_seconds`

**Config options:** `count` (packets per run, default 5), `interval` (between packets, default 200ms), `payload_size` (bytes, default 56), `privileged` (raw socket, default true)

### DNS

Resolves records against multiple resolvers simultaneously and compares results. Supports A, AAAA, CNAME, MX, NS, TXT, and SRV record types. Content validation asserts that resolved values match expected values.

**Prometheus metrics:** `netvantage_dns_resolution_seconds{target, pop, resolver}`, `netvantage_dns_response_code{target, pop, resolver}`

**Config options:** `record_type` (default A), `resolvers` (list of resolver IPs, default system resolver), `expected_values` (optional content assertion)

### HTTP/S

Full HTTP timing breakdown via `httptrace.ClientTrace` (DNS/TCP/TLS/TTFB/transfer/total), status code assertion, content matching (string and regex), TLS certificate validation and expiry monitoring, redirect chain tracking. Supports GET/POST/HEAD with custom headers and body.

**Prometheus metrics:** `netvantage_http_duration_seconds{target, pop, phase}`, `netvantage_http_status_code{target, pop}`, `netvantage_http_cert_expiry_days{target, pop}`

**Config options:** `method` (default GET), `headers`, `body`, `expected_status` (default 200), `content_match`, `content_regex`, `follow_redirects` (default true), `max_redirects` (default 10), `tls_skip_verify`, `validate_tls` (default true)

### Traceroute

Hop-by-hop network path mapping via `mtr --json` (default) or `scamper` (Paris traceroute). Dual backend support with configurable cycles (default 10), max hops, packet size, and probe protocol (ICMP/UDP/TCP). Per-hop metrics: IP, RTT (min/avg/max/stddev), packet loss, ASN, hostname. AS path extraction for BGP correlation. Path change detection between consecutive runs alerts on routing shifts. Configurable hostname resolution and ASN lookup.

**Prometheus metrics:** `netvantage_traceroute_hop_rtt_seconds{target, pop, hop, hop_ip, hop_asn}`, `netvantage_traceroute_hop_loss_ratio{target, pop, hop, hop_ip}`, `netvantage_traceroute_hop_count{target, pop}`, `netvantage_traceroute_path_change_total{target, pop}`, `netvantage_traceroute_reachable{target, pop}`

**Config options:** `backend` (mtr or scamper, default mtr), `cycles` (default 10), `max_hops` (default 30), `packet_size` (default 64), `protocol` (icmp/udp/tcp, default icmp), `port`, `interval_sec` (default 0.25), `resolve_asn` (default true), `resolve_hostnames` (default true)

## Grafana Dashboards

All dashboards are provisioned as code from `grafana/dashboards/` — no manual creation. They auto-load when the stack starts.

| Dashboard | File | Description |
|---|---|---|
| Home | `home.json` | Landing page with links to all dashboards |
| BGP Event Timeline | `bgp-events.json` | Chronological BGP events, filterable by prefix/origin AS/severity/RPKI status |
| Ping Overview | `ping-overview.json` | RTT heatmap, packet loss time series, jitter trends, per-target status |
| DNS Overview | `dns-overview.json` | Resolution time histograms, response code distribution, resolver comparison |
| HTTP Overview | `http-overview.json` | Response time breakdown, status codes, TLS cert expiry, TTFB |
| Traceroute Overview | `traceroute-overview.json` | Per-hop latency heatmap, reachability, path changes, hop loss |
| Platform Health | `platform-health.json` | Agent heartbeats, NATS consumer lag, processing rates, API latency |

## Alerting

Prometheus alert rules live in `prometheus/rules/` and route through Alertmanager to Slack, PagerDuty, email, or webhooks.

| Alert | Severity | Condition |
|---|---|---|
| `NetVantagePingHighLatency` | warning | RTT > 200ms for 5m |
| `NetVantagePingCriticalLatency` | critical | RTT > 500ms for 2m |
| `NetVantagePingTargetUnreachable` | critical | 100% packet loss for 2m |
| `NetVantagePingHighPacketLoss` | warning | > 20% loss for 5m |
| `NetVantagePingHighJitter` | warning | Jitter > 50ms for 5m |
| `NetVantageAgentDown` | critical | No results from agent for 5m |
| `NetVantageDNSResolutionFailure` | warning | Non-NOERROR response for 2m |
| `NetVantageDNSSlowResolution` | warning | Resolution > 500ms for 5m |
| `NetVantageDNSCriticalResolution` | critical | Resolution > 2s for 2m |
| `NetVantageBGPHijackDetected` | critical | Unexpected origin AS for monitored prefix |
| `NetVantageBGPPrefixWithdrawal` | warning | Monitored prefix withdrawn |
| `NetVantageBGPAnalyzerStale` | warning | No BGP updates for 10m |
| `NetVantageHTTP5xx` | critical | HTTP 5xx status for 2m |
| `NetVantageHTTPHighLatency` | warning | HTTP total > 2s for 5m |
| `NetVantageHTTPCriticalLatency` | critical | HTTP total > 5s for 2m |
| `NetVantageTLSCertExpiry30d` | warning | TLS cert expires within 30 days |
| `NetVantageTLSCertExpiry14d` | warning | TLS cert expires within 14 days |
| `NetVantageTLSCertExpiry7d` | critical | TLS cert expires within 7 days |
| `NetVantageTLSCertExpiry1d` | critical | TLS cert expires tomorrow |
| `NetVantageTracerouteUnreachable` | warning | Target unreachable via traceroute for 5m |
| `NetVantageTracerouteUnreachableCritical` | critical | Target unreachable via traceroute for 15m |
| `NetVantageTraceroutePathChange` | warning | AS path change detected |
| `NetVantageTracerouteHighHopLoss` | warning | > 50% loss at a hop for 10m |
| `NetVantageTracerouteHighLatencyHop` | warning | > 500ms RTT at a hop for 10m |
| `NetVantageProcessorDown` | critical | Metrics Processor unreachable for 2m |
| `NetVantageHighProcessingErrorRate` | warning | Error rate > 0.1/s for 5m |

## Database

PostgreSQL schema is managed via numbered migration files in `migrations/`. The initial schema (`001_initial_schema.sql`) creates tables for POPs, agents, test definitions, test assignments, and API keys — all with indexes, foreign key constraints, and auto-updating `updated_at` triggers.

Run migrations against your database:

```bash
psql $NETVANTAGE_DB_URL -f migrations/001_initial_schema.sql
```

Migrations are idempotent (`IF NOT EXISTS`, `ON CONFLICT`) and safe to re-run.

## Project Status

NetVantage is in active early development. The BGP analyzer — our primary competitive differentiator — ships first because it has zero dependency on the Go agent pipeline.

| Milestone | Status | Description |
|---|---|---|
| M1: Scaffolding | ✅ Complete | Project structure, transport abstraction, dev stack |
| M2: BGP Analysis | ✅ Complete | Hijack detection, RPKI validation, BGP dashboards |
| M3: Ping Canary | ✅ Complete | First canary type, end-to-end pipeline proof |
| M4: DNS Canary | ✅ Complete | DNS monitoring with resolver comparison |
| M5: Control Plane | ✅ Complete | Centralized agent management API |
| M6: HTTP/S Canary | ✅ Complete | Web monitoring with TLS validation |
| M7: Traceroute | ✅ Complete | Hop-by-hop path mapping with AS path detection |
| M8: BGP+Traceroute | 🔜 Next | AS path correlation engine |
| M9: Hardening | Planned | Kafka backend, Protobuf, Helm, security |
| M10: Release Prep | Planned | Dashboard suite, docs, release gates |

## Documentation

| Doc | What You'll Learn |
|---|---|
| **[Understanding NetVantage](docs/concepts.md)** | Start here. The problem space, BGP, RPKI, and how everything fits together — no networking background required. |
| **[Guided Demo](docs/quickstart.md)** | Get the full stack running locally with explanations at every step. |
| **[BGP Monitoring Demo](docs/quickstart-bgp.md)** | Set up hijack detection, RPKI validation, and explore the BGP dashboard. |
| **[Architecture](docs/ARCHITECTURE.md)** | Technical deep dive with design rationale for every decision. |
| **[Contributing](docs/CONTRIBUTING.md)** | Development workflow, conventions, and how to add canary types. |

## Tech Stack

| Component | Technology |
|---|---|
| Agent & Control Plane | Go 1.22+ (`net/http`, chi, pgx) |
| BGP Analyzer | Python 3.12 (pybgpstream, structlog) |
| Message Transport | NATS JetStream (default) · Kafka (production) |
| Metrics & Visualization | Prometheus + Grafana |
| RPKI Validation | Routinator |
| Database | PostgreSQL 16 |
| CI/CD | GitHub Actions |

## Project Layout

```
cmd/
  agent/          # Canary agent entry point
  server/         # Control plane API entry point
  processor/      # Metrics processor entry point
internal/
  agent/          # Agent lifecycle, config, buffer
    canary/       # Canary interface + implementations (ping, dns, ...)
  server/         # Control plane (handlers, middleware, repos, router)
  transport/      # Transport abstraction (NATS, Kafka, memory)
  processor/      # Result consumption + Prometheus write
  domain/         # Shared domain models and errors
bgp-analyzer/     # Python BGP analysis service (independent lifecycle)
migrations/       # PostgreSQL schema migrations
grafana/          # Dashboard JSON + provisioning config
prometheus/       # Scrape config + alert rules
alertmanager/     # Alert routing config
deploy/           # Helm charts, Terraform modules, Ansible playbooks
docs/             # All documentation
```

## Roadmap

### V1: Ship a Functional Platform

Every canary type ships with its corresponding Grafana dashboard, Prometheus alert rules, and documentation — no feature ships without observability. The BGP analyzer ships first because it's the primary competitive differentiator and has zero dependency on the Go agent pipeline.

#### M1: Project Scaffolding & Dev Environment ✅

Go module structure (`cmd/agent/`, `cmd/server/`, `cmd/processor/`, `internal/`), Python project (`bgp-analyzer/`), transport abstraction (`Publisher`/`Consumer` interfaces) with NATS JetStream and in-memory implementations, Docker Compose dev stack (NATS, Prometheus, Grafana, PostgreSQL, Alertmanager, Routinator), CI pipeline (GitHub Actions for Go and Python), agent lifecycle skeleton (startup → registration → config sync → execution loop → heartbeat → graceful shutdown), canary interface, local result buffer for transport-down resilience, config caching for offline agent operation, Taskfile with all dev workflow targets, BSL 1.1 license file.

#### M2: BGP Analysis Engine ✅

The competitive differentiator — live and demo-able before any canary exists. pybgpstream integration subscribing to RouteViews/RIPE RIS, configurable IPv4/IPv6 prefix monitoring, event detection (announcements, withdrawals, AS path changes, origin AS changes), hijack detection (unexpected origin AS, MOAS conflicts, sub-prefix hijacks), RPKI Route Origin Validation via Routinator HTTP API with `valid`/`invalid`/`not-found` tagging, ROA lifecycle monitoring (expiry countdown at 30/14/7/1 day thresholds, ROA deletion/creation alerts), Prometheus metrics (`netvantage_bgp_event_total`, `netvantage_bgp_rpki_status`, `netvantage_bgp_roa_expiry_days`), BGP Event Timeline Grafana dashboard, alert rules (hijack critical, withdrawal warning, analyzer staleness, RPKI-invalid announcement, ROA expiry), Alertmanager routing for Slack + email, tests using recorded MRT data fixtures + mock Routinator responses, standalone quickstart guide.

#### M3: Ping Canary ✅

First canary type proving the full Go pipeline end-to-end: agent → NATS → Metrics Processor → Prometheus → Grafana. ICMP ping via pro-bing with configurable targets, packet count, interval, timeout, payload size. Prometheus metrics (`netvantage_ping_rtt_seconds`, `netvantage_ping_packet_loss_ratio`, `netvantage_ping_jitter_seconds`), Ping Overview Grafana dashboard (RTT heatmap, packet loss, jitter trends, status table), alert rules (high latency, target unreachable, agent down, high jitter), table-driven unit tests, integration test covering the full pipeline.

#### M4: DNS Canary ✅

DNS monitoring with cross-resolver comparison. A/AAAA/CNAME/MX/NS/TXT/SRV queries against custom resolver targets, content validation (assert expected resolved values), error classification (NXDOMAIN, SERVFAIL, TIMEOUT, REFUSED). Prometheus metrics (`netvantage_dns_resolution_seconds`, `netvantage_dns_response_code`), DNS Overview Grafana dashboard (resolution time histograms, NXDOMAIN/SERVFAIL rates, resolver comparison), alert rules (failure rates, slow/critical resolution).

#### M5: Control Plane API ✅

Centralized management — no more hardcoded agent configs. Go REST API with chi router, PostgreSQL-backed (pgx connection pooling, raw SQL, numbered migration files). Agent registration with POP metadata, test CRUD, test assignment to POPs/POP groups, agent config sync (pull assigned tests on interval, cache locally for offline resilience), agent heartbeat tracking, API key auth with SHA-256 hashing and role-based scopes, per-IP rate limiting, request logging, Platform Health Grafana dashboard (agent heartbeats, NATS consumer lag, API latency), OpenAPI spec planned.

#### M6: HTTP/S Canary ✅

Web service monitoring with full timing breakdown. HTTP/S canary with GET/POST/HEAD, custom headers, body, auth. Timing via `httptrace.ClientTrace`: DNS/TCP/TLS/TTFB/total. Status code assertion, content matching (string/regex), TLS cert validation and expiry countdown, redirect chain tracking. Prometheus metrics (`netvantage_http_duration_seconds{phase}`, `netvantage_http_status_code`), HTTP Overview Grafana dashboard, alert rules for 5xx errors and TLS cert expiry at 30/14/7/1 days.

#### M7: Traceroute Canary ✅

Hop-by-hop network path mapping with dual backend support. `mtr --json` default with `scamper` optional (Paris traceroute). Per-hop metrics: IP, RTT (min/avg/max/stddev), packet loss, ASN, hostname. Path change detection between consecutive runs via AS path comparison. Configurable cycle count (default 10), max hops, packet size, probe protocol (ICMP/UDP/TCP). Prometheus metrics (`netvantage_traceroute_hop_rtt_seconds`, `netvantage_traceroute_hop_loss_ratio`, `netvantage_traceroute_hop_count`, `netvantage_traceroute_path_change_total`, `netvantage_traceroute_reachable`), Traceroute Overview Grafana dashboard with per-hop latency heatmap, reachability status, path change timeline, hop RTT/loss time series, and current path table. Alert rules for unreachable targets, AS path changes, high per-hop loss, and high per-hop latency.

#### M8: BGP + Traceroute Correlation 🔜

The feature that justifies having both BGP and traceroute in one platform. AS path reconstruction from traceroute hop ASN data, correlation engine comparing reconstructed AS path against BGP-observed AS path for the same prefix, discrepancy detection alerting when traceroute diverges from BGP announcements. Reveals route leaks, traffic engineering issues, and hijacks that neither system catches alone. Prometheus metrics (`netvantage_path_correlation_mismatch_total{prefix, pop}`), correlated path view in BGP dashboard showing BGP vs. observed paths side-by-side.

#### M9: Production Hardening

Kafka transport backend (SASL/SCRAM or mTLS), JSON → Protobuf migration for transport messages, Grafana OAuth2/OIDC SSO with RBAC, secrets management (Vault/K8s Secrets/SOPS), binary signing with cosign/sigstore + SBOM generation, Helm chart with persistent volumes and NetworkPolicy defaults, Prometheus/Alertmanager UIs behind authed reverse proxy, audit logging on all Control Plane mutations, POP deployment guides (AWS, GCP, Azure, bare-metal), load testing at 100+ simulated POPs.

#### M10: Dashboard Suite & Release Prep

Global Map Dashboard (Grafana Geomap, all POPs color-coded with click-through), Per-Target Drill-Down Dashboard (all canary types combined, multi-POP comparison, p50/p95/p99), POP Comparison Dashboard (side-by-side performance), complete documentation suite (quickstart through security hardening), all release gate criteria verified.

#### v1.0.0 Release Gates

All must pass before tagging v1.0.0:

- BGP Analyzer detecting hijacks and routing anomalies
- Four canary types operational end-to-end (ping, DNS, HTTP, traceroute)
- BGP + Traceroute path correlation detecting AS path discrepancies
- Control Plane API with auth, test CRUD, agent registration, config sync
- 10 Grafana dashboards deployed and provisioned as code
- Alerting suite with Alertmanager routing to Slack, PagerDuty, email, webhooks
- NATS JetStream default; Kafka available as production backend
- Grafana SSO, secrets management, transport encryption
- Helm chart validated; Docker Compose for small deployments
- Signed binaries/images, SBOM published
- Documentation complete
- CI/CD pipeline green (lint, test, build, sign)
- No known critical or high-severity bugs
- BSL 1.1 license reviewed and finalized by legal

---

### V2: Scale, Community & Commercial Readiness

#### V2.0: BGP Analyzer v2 & Multi-Tenancy

AS path change tracking with rolling window and path length anomaly detection, severity classification engine, internal BGP feeds via OpenBMP/ExaBGP for private router monitoring, RPKI advanced intelligence (ROA recommendation engine, ASPA validation, RPKI-weighted hijack confidence scoring), route leak detection via CAIDA AS-relationship data, prefix reachability scoring, multi-collector event deduplication, BGP community tracking, notification enrichment (ASN→org name via PeeringDB, dashboard deeplinks in alerts). Multi-tenancy with organization/workspace isolation and audit logging. SLA/SLO Tracking Dashboard with error budget burn rate.

#### V2.1: POP Automation & Community Ecosystem

One-liner agent install script with auto-registration, Terraform modules (AWS, GCP, Azure), Ansible playbooks for bare-metal deployment, canary developer SDK for community extensions, public POP program design (community-contributed vantage points).

#### V2.2: Commercial Launch

Commercial license purchasing flow, enterprise support subscriptions, backup/restore documentation for all stateful components.

---

### V3+ Future Directions

AI/ML anomaly detection on metrics streams, automated root cause analysis (cross-canary cross-POP correlation), interactive network path visualization (traceroute + BGP topology overlay), agent auto-update with rolling self-update and rollback, API-first managed cloud offering, integration marketplace (Terraform provider, CLI tool, ChatOps bots), HTTP/2 + HTTP/3 (QUIC) + DoH/DoT canary support, IPv6 parity, Paris traceroute via scamper, historical BGP event store (ClickHouse) for post-incident forensics.

## License

NetVantage is source-available under the [Business Source License 1.1](LICENSE). Free for non-competing production use. Converts to Apache 2.0 four years after each release.

Use it for monitoring your own infrastructure, contribute to it, build internal tools with it — all fine. Don't resell it as a competing managed monitoring service.
