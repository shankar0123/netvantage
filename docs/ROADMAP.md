# NetVantage — V1 Roadmap

**Last updated:** 2026-03-20
**Governing principle:** Every feature ships with its Grafana dashboard, Prometheus alert rules, and documentation — or it doesn't ship.
**Strategic sequencing:** BGP analysis is our primary competitive differentiator and ships first (M2). No other open-source or freemium tool combines distributed synthetic monitoring with BGP analysis. Our moat activates the moment BGP goes live.

---

## Milestone Sequence

```
M1  Scaffolding        ✅ repo structure, Docker Compose, CI, agent skeleton, transport abstraction
M2  BGP Analyzer v1    ✅ ⭐ DIFFERENTIATOR — hijack detection, routing anomalies, dashboard, alerts
M3  Ping Canary        ✅ first Go canary end-to-end (agent → NATS → processor → Prometheus → Grafana)
M4  DNS Canary         ✅ resolver comparison, content validation
M5  Control Plane      ✅ agent registration, test CRUD, config sync, auth
M6  HTTP/S Canary      ✅ timing breakdown, TLS validation, content matching
M7  Traceroute Canary  ✅ hop-by-hop path mapping, AS enrichment, path change detection
M8  BGP+TR Correlation ⬜ compare BGP-announced AS paths vs. traceroute-observed paths ← NEXT
M9  Hardening          ⬜ Kafka backend, Protobuf, security, Helm, load testing
M10 Release Prep       ⬜ dashboard suite, docs, release gates
```

---

## Phase 1: Foundation (M1–M4)

**Goal:** BGP differentiator live + core probe pipeline proven with Ping and DNS canaries.

### M1: Project Scaffolding & Dev Environment

**Deliverables:**
- Go module: `cmd/agent/`, `cmd/server/`, `cmd/processor/`, `internal/`
- Python project: `bgp-analyzer/` with pyproject.toml, Dockerfile, CI job
- Transport abstraction: `Publisher`/`Consumer` interfaces with NATS JetStream implementation
- In-memory transport for unit tests
- Docker Compose: NATS, Prometheus, Grafana, PostgreSQL, Alertmanager, Routinator (RPKI validator) — persistent volumes
- CI pipeline (GitHub Actions): Go (`lint` → `test` → `build` → `vet`) + Python (`lint` → `test` → `build`)
- Agent lifecycle skeleton: startup → registration → config sync → execution loop → heartbeat → graceful shutdown
- Canary interface (Go interface, NOT `plugin.Open`)
- Local result buffer (disk-backed) for transport-down resilience
- Config caching for offline agent operation
- Taskfile: `dev-up`, `dev-down`, `build-agent`, `build-server`, `build-processor`, `build-bgp`, `test`, `test-integration`, `lint`, `dashboards-validate`
- BSL 1.1 `LICENSE` file
- `PROJECT_STATE.md` template (gitignored)

### M2: BGP Analysis Engine v1 ⭐

**Why first:** BGP analysis is what separates NetVantage from Cloudprober, Blackbox Exporter, Uptime Kuma, and every other open-source probe tool. It's an independent Python service with zero dependency on the Go agent pipeline — only needs Prometheus + Grafana + Routinator from M1 Docker Compose. Ship the moat first.

**Deliverables:**
- BGP Analyzer: subscribe to RouteViews/RIPE RIS via pybgpstream (BGP-01)
- Configurable prefix monitoring, IPv4 and IPv6 (BGP-02)
- Event detection: announcements, withdrawals, AS path changes, origin AS changes (BGP-03)
- Hijack detection: unexpected origin AS, MOAS conflicts, sub-prefix hijacks (BGP-04, BGP-05)
- RPKI Route Origin Validation: query Routinator HTTP API per announcement, tag with ROA status (`valid`, `invalid`, `not-found`) (BGP-08)
- RPKI-invalid announcement alerting (ALT-13)
- ROA lifecycle monitoring: alert on ROA expiry (30/14/7/1 day), deletion, new ROA creation for monitored prefixes (BGP-13)
- Prometheus metrics: `netvantage_bgp_event_total`, `netvantage_bgp_analyzer_last_update`, `netvantage_bgp_rpki_status{prefix, origin_asn, status}`, `netvantage_bgp_roa_expiry_days{prefix}` (BGP-06, BGP-08, BGP-12, BGP-13)
- Grafana BGP Event Timeline Dashboard (DASH-06): filterable by RPKI status; ROA expiry countdown panel
- Alert rules: BGP hijack critical (ALT-06), withdrawal (ALT-07), analyzer staleness (ALT-08), RPKI-invalid (ALT-13), ROA expiry (ALT-14)
- Alertmanager routing: Slack webhook + email defaults (ALT-12)
- Tests using recorded BGP data fixtures (MRT dumps) + mock Routinator responses
- Own Dockerfile, own CI job, isolated at `bgp-analyzer/`
- `docs/quickstart-bgp.md` — standalone quickstart

### M3: Ping Canary — End-to-End

**Deliverables:**
- ICMP Ping canary: configurable targets, packet count, interval, timeout, payload size (PING-01 through PING-04)
- NATS producer in agent; NATS consumer in Metrics Processor → Prometheus via remote_write
- Test result JSON schema (Protobuf migration in M9)
- Prometheus metrics: `netvantage_ping_rtt_seconds`, `netvantage_ping_packet_loss_ratio`
- Grafana Ping Overview Dashboard (DASH-02)
- Alert rules: high latency (ALT-01), target unreachable (ALT-03), agent down (ALT-10)
- Table-driven unit tests; integration test for full pipeline

### M4: DNS Canary

**Deliverables:**
- DNS canary: A/AAAA/CNAME/MX/NS/TXT/SOA/SRV queries, custom resolver targets (DNS-01 through DNS-03)
- Content validation: assert expected resolved values (DNS-04)
- Prometheus metrics: `netvantage_dns_resolution_seconds`, `netvantage_dns_response_code`
- Grafana DNS Overview Dashboard (DASH-03)
- Alert rules: DNS failure rates (ALT-02)

---

## Phase 2: Full Canary Suite + Control Plane (M5–M8)

**Goal:** All four canary types operational, centralized management, BGP+traceroute correlation.

### M5: Control Plane API v1

**Deliverables:**
- Go REST API (`net/http` + `chi`): agent registration, test CRUD, test assignment (CP-01 through CP-04)
- PostgreSQL schema: test definitions, agent inventory, POP metadata
- JWT + API key auth, scoped permissions (CP-05)
- Agent config sync with local caching
- Rate limiting, input validation (CP-08)
- OpenAPI spec (CP-09)
- Platform Health Dashboard (DASH-09)
- Integration tests against real PostgreSQL (testcontainers-go)

### M6: HTTP/S Canary

**Deliverables:**
- HTTP/S canary: GET/POST/HEAD with custom headers, body, auth (HTTP-01)
- Timing breakdown via `httptrace.ClientTrace`: DNS/TCP/TLS/TTFB/total (HTTP-02)
- Status code assertion, content matching, TLS cert validation (HTTP-03 through HTTP-05)
- Redirect chain tracking (HTTP-06)
- Prometheus metrics: `netvantage_http_duration_seconds{phase}`, `netvantage_http_status_code`
- Grafana HTTP Overview Dashboard (DASH-04)
- Alert rules: HTTP 5xx (ALT-04), TLS cert expiry (ALT-05)

### M7: Traceroute Canary

**Deliverables:**
- Traceroute canary: `mtr --json` default, `scamper` optional (TR-01 through TR-03)
- Per-hop metrics: IP, RTT, packet loss, ASN, geolocation, reverse DNS (TR-03)
- Metrics Processor: flatten hop-by-hop arrays to per-hop Prometheus metrics (TR-04)
- Path change detection (TR-05)
- Prometheus metrics: `netvantage_traceroute_hop_rtt_seconds`, `netvantage_traceroute_path_change_total`
- Grafana Traceroute Dashboard (DASH-05)

### M8: BGP + Traceroute Correlation

**Why its own milestone:** This is the feature that justifies having both BGP and traceroute in one platform. Detecting discrepancies between BGP-announced AS paths and traceroute-observed AS paths is ThousandEyes-grade capability.

**Deliverables:**
- AS path reconstruction from traceroute hop ASN data (TR-08)
- Correlation engine: compare reconstructed vs. BGP-observed AS paths
- Discrepancy detection and alerting
- Prometheus metrics: `netvantage_path_correlation_mismatch_total{prefix, pop}`
- Grafana panel: correlated path view (BGP vs. observed)
- Integration tests with recorded BGP + traceroute data pairs

---

## Phase 3: Production Readiness (M9–M10)

**Goal:** Security hardening, Kubernetes deployment, complete documentation, release.

### M9: Production Hardening

**Deliverables:**
- Kafka transport backend via `Publisher`/`Consumer` interfaces (SEC-01)
- JSON → Protobuf migration for transport messages
- Grafana OAuth2/OIDC SSO, RBAC (SEC-03)
- Secrets management: Vault/K8s Secrets/SOPS (SEC-07)
- Binary signing (cosign/sigstore), SBOM (SEC-06)
- Helm chart with persistent volumes, resource limits, NetworkPolicy (SEC-08)
- Prometheus/Alertmanager behind authed reverse proxy (SEC-05)
- Audit logging (SEC-09)
- POP deployment docs: AWS, GCP, Azure, bare-metal (POP-01)
- Network requirements docs (POP-08)
- Load testing at 100+ simulated POPs

### M10: Dashboard Suite & Release Prep

**Deliverables:**
- Global Map Dashboard: Geomap panel (DASH-01)
- Per-Target Drill-Down Dashboard: multi-POP comparison, p50/p95/p99 (DASH-07)
- POP Comparison Dashboard (DASH-08)
- Documentation: quickstart, architecture, API reference, canary dev guide, POP deployment, security hardening
- All release gate criteria verified

---

## v1.0.0 Release Gates

All must be true:

- [x] BGP Analyzer v1 with RPKI validation detecting hijacks and routing anomalies (M2) ✅
- [x] Four canary types operational end-to-end: ping, DNS, HTTP, traceroute (M3–M7) ✅
- [ ] BGP + Traceroute path correlation detecting AS path discrepancies (M8)
- [x] Control Plane API with auth, test CRUD, agent registration, config sync (M5) ✅
- [ ] 10 Grafana dashboards deployed and provisioned as code (7 of 10 done)
- [x] Alerting suite with Alertmanager routing to Slack, PagerDuty, email, webhooks ✅
- [ ] NATS JetStream default transport; Kafka available as production backend (M9)
- [ ] Grafana SSO, secrets management, transport encryption (M9)
- [ ] Helm chart validated; Docker Compose for small deployments (M9)
- [ ] Signed binaries/images, SBOM published (M9)
- [ ] Documentation complete: quickstart through security hardening (M10)
- [x] CI/CD: lint, test, build pipeline green ✅
- [ ] No known critical or high-severity bugs
- [ ] BSL 1.1 license reviewed and finalized by legal

---

## V2 Roadmap: "Scale, Community & Commercial Readiness"

### V2.0: BGP Analyzer v2 & Multi-Tenancy

**BGP Enhancements:**
- AS path change tracking with rolling window and path length anomaly detection (BGP-09)
- Severity classification engine (BGP-11)
- Internal BGP feeds via OpenBMP/ExaBGP for private router monitoring (BGP-10)
- RPKI advanced intelligence: ROA recommendation engine, ASPA validation (draft spec), RPKI-weighted hijack confidence scoring (BGP-14)
- Route leak detection: heuristic valley-free violation detection using CAIDA AS-relationship data (BGP-15)
- Prefix reachability scoring: percentage of collectors seeing each prefix as health metric (BGP-16)
- Multi-collector correlation: cross-collector event deduplication to reduce false positives (BGP-17)
- BGP community tracking: monitor community attribute changes on monitored prefixes (BGP-18)
- Notification enrichment: ASN→org name (PeeringDB), RPKI status, collector count, dashboard deeplink in alerts (BGP-19)

**Multi-Tenancy:**
- Organization/workspace isolation for test configs and results (CP-06)
- Audit logging with actor, timestamp, source IP, change diff (CP-07)

**Dashboards:**
- SLA/SLO Tracking Dashboard: error budget burn rate, uptime %, breach predictions (DASH-10)

### V2.1: POP Automation & Community Ecosystem

- One-liner agent install script with auto-registration (POP-02)
- Cloud provider Terraform modules: AWS, GCP, Azure (POP-04)
- Ansible playbooks for bare-metal POP deployment (POP-05)
- Canary developer SDK and documentation for community extensions
- Public POP program design (community-contributed vantage points)

### V2.2: Commercial Launch

- Commercial license purchasing flow
- Enterprise support subscriptions
- Backup/restore documentation and scripts for all stateful components (INF-06)

---

## Future Directions (V3+)

_Directional, not committed. Scope based on V1/V2 learnings and user demand._

- AI/ML anomaly detection on metrics streams
- Automated root cause analysis (cross-canary, cross-POP correlation)
- Interactive network path visualization (traceroute + BGP topology overlay)
- Agent auto-update with rolling self-update and rollback
- API-first managed cloud offering with hosted POP infrastructure
- Integration marketplace (Terraform provider, CLI tool, ChatOps bots)
- HTTP/2, HTTP/3 (QUIC), DoH/DoT canary support
- IPv6 parity across all canary types
- Paris traceroute via scamper for load-balanced path detection
- Historical BGP event store (ClickHouse) for post-incident forensics
