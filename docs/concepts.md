# Understanding NetVantage — From Zero

This guide is for anyone who wants to understand what NetVantage does and why it exists, even if you've never heard of BGP, synthetic monitoring, or route hijacking. By the end, you'll understand the problem space, how the pieces fit together, and why we built things the way we did.

## The Problem: You Can't Monitor What You Can't See

Imagine you run a website. Your servers are healthy, your code is fine, but users in Tokyo can't load your page. The problem isn't your infrastructure — it's the *network between* your servers and those users. A router in Singapore is misconfigured and sending your traffic into a black hole.

This happens constantly. The internet is a patchwork of 70,000+ independently operated networks, and any one of them can silently break your users' experience. The challenge is that traditional monitoring (uptime checks, server health) only watches your own infrastructure. Nobody's watching the network paths your traffic actually takes.

That's the first problem NetVantage solves: **synthetic monitoring from multiple vantage points.** Instead of only checking "is my server up?", you deploy lightweight agents across different networks and geographies. Each agent performs tests (pings, DNS lookups, HTTP requests, traceroutes) against your targets and reports back. If Tokyo can reach you but Singapore can't, you see exactly where the breakdown is.

## The Bigger Problem: BGP and Route Hijacking

But there's a deeper problem. The internet's routing system — BGP (Border Gateway Protocol) — is built on trust. When your network announces "I own IP block 203.0.113.0/24," every other network just believes you. There's no built-in verification.

This means anyone can announce *your* IP addresses and steal your traffic. It happens accidentally (a misconfigured router leaks internal routes) and maliciously (an attacker redirects your users' traffic through their network to intercept it). These are called **BGP hijacks**, and they're one of the internet's most dangerous and least visible attack vectors.

Some real-world examples of what can go wrong:

**Prefix hijack** — Someone announces your exact IP block from their network. Traffic that should reach you goes to them instead. If they're malicious, they can intercept, modify, or drop it.

**Sub-prefix hijack** — Even sneakier. If you announce 203.0.113.0/24, an attacker announces 203.0.113.0/25 and 203.0.113.128/25. In BGP, more-specific routes always win. The attacker captures all your traffic without you seeing anything wrong on your end.

**MOAS conflict (Multiple Origin AS)** — Two different networks both claim to originate the same prefix. Sometimes this is legitimate (content delivery networks do this), but often it's a sign of a hijack or misconfiguration.

**Route leak** — A network accidentally re-announces routes it learned from one peer to another peer that shouldn't receive them. This can cause traffic for entire countries to be rerouted through a single small ISP.

The only way to detect these attacks is to watch the global routing table — the stream of BGP announcements flowing between networks worldwide. Several academic projects (RouteViews, RIPE RIS) collect this data from hundreds of observation points and make it available publicly. NetVantage subscribes to these streams and watches for anything suspicious involving your prefixes.

## The Defense Layer: RPKI

The internet community has been building a defense against BGP hijacks called **RPKI (Resource Public Key Infrastructure)**. Here's how it works:

You own an IP block (say, 203.0.113.0/24). You create a **Route Origin Authorization (ROA)** — a cryptographically signed statement that says "AS 64500 is authorized to announce 203.0.113.0/24." This ROA is published in a global, verifiable database.

Networks that enforce RPKI can then check every BGP announcement against these ROAs. If someone announces your prefix from an unauthorized AS, their announcement is tagged as **RPKI-invalid** and can be automatically rejected.

NetVantage runs **Routinator** — an RPKI validator — and checks every BGP announcement for your monitored prefixes against the current ROA set. If an announcement is RPKI-invalid, you get an immediate critical alert. NetVantage also monitors your ROAs themselves: if a ROA is about to expire, you get warned at 30, 14, 7, and 1 day thresholds. An expired ROA means RPKI-enforcing networks might start rejecting your legitimate announcements.

## How NetVantage Puts It All Together

NetVantage is two systems working together, plus a correlation engine that connects them.

### System 1: Synthetic Monitoring (Go)

Small agents deployed at your network points of presence (POPs) — cloud VMs, edge nodes, data centers. Each agent runs configurable synthetic tests:

**Ping** — Is this target reachable? How fast? How much packet loss? Think of this as the most basic health check: "can I reach this IP, and how long does it take?"

**DNS** — Can my resolver find this domain? Is it returning the right answer? DNS is the first step in almost every internet interaction, so if it breaks, everything downstream breaks too.

**HTTP/S** — Can I load this URL? How long does each phase take (DNS lookup → TCP connection → TLS handshake → first byte → full response)? Is the TLS certificate valid and not expiring?

**Traceroute** — What path does my traffic take to reach this target? Which routers does it pass through? What AS (autonomous system / network) owns each hop? Traceroute turns an invisible network path into a visible, measurable sequence of hops.

Results flow through a message bus (NATS JetStream) to a metrics processor, which transforms them into Prometheus time-series data. Grafana dashboards visualize everything. Alertmanager fires alerts when things go wrong.

**Why this architecture?** Agents need to be tiny and resilient — they're running on potentially unreliable remote networks. A single Go binary with no dependencies fits perfectly. The message bus decouples agents from the processing pipeline, so agents keep running even if the hub is temporarily down. Results are buffered locally on disk and replayed when connectivity resumes. No data is ever lost.

### System 2: BGP Analysis (Python)

A separate service that subscribes to live BGP data from RouteViews and RIPE RIS collectors worldwide. It watches for events involving your monitored prefixes:

- New announcements and withdrawals
- Origin AS changes (the network claiming to own a prefix changes)
- AS path changes (the route traffic takes to reach a prefix changes)
- Hijack indicators (unexpected origin, MOAS conflict, sub-prefix announcement)
- RPKI validation status of every announcement

**Why Python?** The best BGP data library (pybgpstream) is Python-native. BGP analysis has a completely different lifecycle from the Go monitoring pipeline — different dependencies, different release cadence, different operational profile. Keeping it as a separate service means each system can evolve independently.

**Why is this separate from the agents?** BGP monitoring is passive — you subscribe to public data streams. You don't need agents deployed anywhere to do it. A single instance watches the global routing table for all your prefixes. It's architecturally independent from the synthetic monitoring pipeline: it just writes metrics straight to Prometheus.

### The Correlation Engine (M8)

Here's where it gets powerful. Traceroute gives you the actual AS-level path traffic takes: "my packet went through AS 64501, then AS 64502, then arrived at AS 64500." BGP tells you the *announced* AS path: "BGP says traffic to this prefix should go through AS 64501 → AS 64500."

When these don't match, something interesting is happening. Maybe a route leak is sending your traffic on a detour. Maybe a hijack is silently intercepting your traffic through a network that shouldn't be in the path. Neither BGP monitoring nor traceroute alone would catch this — but comparing them reveals the discrepancy.

This BGP + traceroute correlation is the feature that justifies building both systems in one platform. It's what commercial tools like ThousandEyes charge $10k+/year for.

## Why Not Just Use Existing Tools?

There are good open-source monitoring tools. Here's what they do and what they're missing:

**Blackbox Exporter / Cloudprober** — Excellent at synthetic probes (ping, HTTP, DNS). Zero BGP awareness. No idea if your traffic is being hijacked.

**Uptime Kuma / Statping** — Simple uptime monitoring with nice UIs. Single vantage point. No distributed monitoring, no BGP, no traceroute.

**BGPalerter** — Good BGP monitoring. No synthetic probes. Can tell you someone hijacked your prefix but can't tell you if your website is slow from Singapore.

**ThousandEyes (Cisco)** — Does everything. Costs $10k+/year. Closed source. Vendor lock-in.

NetVantage is the first open-source (source-available) tool that combines distributed synthetic monitoring with BGP analysis and RPKI validation in a single platform. If you're an infrastructure team, network engineer, or SRE who cares about routing security, this is the tool we wished existed.

## The Observability Stack

Every NetVantage feature follows the same pattern: data flows into **Prometheus** (time-series metrics database), is visualized in **Grafana** (dashboards), and triggers alerts via **Alertmanager** (routing to Slack, PagerDuty, email).

**Why Prometheus + Grafana?** They're the industry standard for infrastructure monitoring. Massive ecosystem, proven at scale, and your team probably already uses them. We don't reinvent the wheel — we feed data into the tools you already know.

**Why dashboard-as-code?** Every dashboard is a JSON file checked into the repo. No one manually creates dashboards in the Grafana UI. This means dashboards are version-controlled, reviewable in pull requests, and automatically deployed. When we ship a new canary type, its dashboard ships with it.

**Why Alertmanager?** Same reason as Prometheus — it's the standard. Alertmanager handles deduplication, grouping, silencing, and routing to multiple notification channels. Our alert rules live in `prometheus/rules/` as YAML files, versioned alongside the code that generates the metrics they alert on.

## Key Design Decisions

These choices were made deliberately, and understanding the reasoning helps you understand the codebase.

**Go for agents, Python for BGP.** Not because one language is better, but because each system has different requirements. Agents need to be tiny, fast, and statically compiled for easy deployment. The BGP ecosystem lives in Python. Forcing both into one language would mean compromises in both.

**NATS JetStream over Kafka for early milestones.** Kafka is the standard for high-throughput message streaming, but it requires a JVM, ZooKeeper (or KRaft), and significant operational overhead. NATS is a single binary, trivially deployable, with persistent streams and at-least-once delivery. For development and deployments under 50 agents, NATS is simpler and lighter. We built a transport abstraction layer so Kafka can slot in for production scale (50+ POPs) with a single config change.

**JSON before Protobuf.** For the first 8 milestones, messages between agents and the processor are JSON. This makes debugging easy — you can read messages directly from NATS. Protobuf brings schema evolution and wire efficiency, but at the cost of debuggability. We migrate in M9 when the system is stable and schema evolution actually matters.

**No ORM, raw SQL.** ORMs hide what's happening in the database. For a system where query performance and schema control matter, raw SQL with `database/sql` gives full visibility and control. Migrations are numbered SQL files that run idempotently.

**BSL 1.1 license.** We wanted NetVantage to be fully source-available — anyone can read, modify, and deploy it for their own infrastructure. The only restriction: you can't take the code and sell it as a competing managed monitoring service. After four years, each release converts to Apache 2.0 (fully open source). This protects the project commercially while keeping it open for users.

## What's Next

If you want to understand the system in more depth, here's the recommended reading order:

1. **[Guided Demo](quickstart.md)** — Get the full stack running locally and see data flowing end to end. Explains every step and why it matters.
2. **[BGP Monitoring Demo](quickstart-bgp.md)** — Set up prefix monitoring, watch for hijacks, and explore the BGP dashboard.
3. **[Architecture](ARCHITECTURE.md)** — Technical deep dive into each component, interfaces, and resilience patterns.
4. **[Roadmap](ROADMAP.md)** — Where we're going, milestone by milestone.
5. **[Contributing](CONTRIBUTING.md)** — How to add code, the conventions we follow, and how to build new canary types.
