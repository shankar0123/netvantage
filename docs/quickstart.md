# Guided Demo: NetVantage Dev Stack

This walkthrough gets the full NetVantage infrastructure running on your machine and explains every step along the way. By the end, you'll have a running observability stack and understand how all the pieces connect.

**New to NetVantage?** Read [Understanding NetVantage](concepts.md) first for a beginner-friendly introduction to the problem space.

## What You'll Set Up

NetVantage's development stack runs six services in Docker containers. Here's what each one does and why it's there:

| Service | What It Does | Why We Need It |
|---|---|---|
| **NATS JetStream** | Message bus between agents and the metrics processor | Agents don't talk to Prometheus directly. They publish test results to NATS, which handles queuing, persistence, and delivery. This decoupling means agents keep running even if the processor is down. |
| **Prometheus** | Stores time-series metrics and evaluates alert rules | The central nervous system for all monitoring data. Every component ultimately writes metrics here. |
| **Grafana** | Visualizes metrics as dashboards | Where you actually look at things. Dashboards are pre-built and provisioned automatically from JSON files in the repo. |
| **PostgreSQL** | Stores agent registrations, test definitions, config | The control plane's persistence layer. Agents register here and pull their test configurations. |
| **Alertmanager** | Routes alerts to Slack, PagerDuty, email | Prometheus detects problems; Alertmanager decides who to notify and how. Handles deduplication and grouping so you don't get flooded. |
| **Routinator** | Validates BGP announcements against RPKI (ROAs) | The BGP analyzer queries Routinator to check if a BGP announcement is authorized. On startup, Routinator syncs the global RPKI database from all five Regional Internet Registries. |

## Prerequisites

You'll need three things installed:

**Docker and Docker Compose (v2)** — This runs all six infrastructure services. If you can run `docker compose version`, you're good.

**Go 1.22+** — For building the agent, server, and processor binaries. These are Go services compiled into single binaries with no runtime dependencies. Check with `go version`.

**[Task](https://taskfile.dev)** — A modern alternative to Make. It reads `Taskfile.yml` in the repo root and provides convenient shortcuts for common operations. Install with `go install github.com/go-task/task/v3/cmd/task@latest` or `brew install go-task`.

## Step 1: Clone and Resolve Dependencies

```bash
git clone https://github.com/shankar0123/netvantage.git
cd netvantage
go mod tidy
```

`go mod tidy` downloads Go dependencies and generates `go.sum` (the lockfile). This step is necessary because the repo doesn't commit `go.sum` — it's generated in CI and locally to avoid platform-specific lockfile conflicts.

## Step 2: Start the Infrastructure Stack

```bash
task dev-up
```

This runs `docker compose up -d` behind the scenes, pulling container images on first run and starting all six services with persistent Docker volumes. Data survives restarts.

Watch the logs to confirm everything starts cleanly:

```bash
task dev-logs
```

**What to look for:** Each service should report it's ready. Routinator will log RPKI sync progress — the initial sync downloads ROA data from AFRINIC, APNIC, ARIN, LACNIC, and RIPE NCC. This takes 2-5 minutes on first run. All other services are ready within seconds.

## Step 3: Explore the Running Services

Open each service and understand what you're looking at:

### Grafana — [http://localhost:3000](http://localhost:3000)

Login with `admin` / `admin`. You'll see the NetVantage home dashboard, which links to all other dashboards.

**Why the home dashboard exists:** When someone first opens Grafana, they should immediately see something useful — not a blank canvas. The home dashboard provides navigation and a quick status overview. It's a JSON file in `grafana/dashboards/home.json`, provisioned automatically on startup.

### Prometheus — [http://localhost:9090](http://localhost:9090)

Click **Status → Targets** to see what Prometheus is scraping. You should see targets for Prometheus itself, NATS, and eventually the BGP analyzer.

**Why check targets?** If a target is "DOWN," metrics from that service aren't being collected. This is the first place to look when a dashboard shows "No Data." Prometheus pulls metrics from services on a schedule (scrape interval) — it doesn't receive pushes.

### NATS Monitoring — [http://localhost:8222](http://localhost:8222)

This is NATS's built-in monitoring endpoint. Navigate to [http://localhost:8222/jsz](http://localhost:8222/jsz) to see JetStream stats — streams, consumers, and message counts.

**Why NATS has monitoring built in:** NATS is designed for operations. The monitoring endpoint tells you how many messages are queued, whether consumers are keeping up, and if any streams are hitting limits. When agents start publishing test results, you'll see message counts increase here.

### Alertmanager — [http://localhost:9093](http://localhost:9093)

Shows active alerts and their current state (pending, firing, resolved). Right now it should be quiet — nothing is generating data yet.

**Why Alertmanager is separate from Prometheus:** Prometheus evaluates rules and detects when conditions are met (e.g., "packet loss > 50% for 5 minutes"). Alertmanager handles the response — who to notify, how to group related alerts, how to avoid sending the same alert twice. Separating these concerns means you can change notification routing without touching alert logic.

## Step 4: Build the Go Services

```bash
task build-agent
task build-server
task build-processor
```

Each command compiles a Go service into a static binary in `bin/`. These binaries have zero runtime dependencies — you can copy them to any Linux machine and run them.

**Why static binaries?** Agents get deployed to remote network points of presence (POPs) — cloud VMs, edge servers, containers. The fewer dependencies, the fewer things that can break during deployment. A single binary means no package managers, no shared libraries, no runtime installations.

## Step 5: Run the Agent Locally

Create a minimal config file `agent.yaml`:

```yaml
agent:
  id: "dev-agent-01"
  pop_name: "local-dev"

transport:
  backend: nats
  nats:
    url: "nats://localhost:4222"

buffer:
  max_size: 1000

heartbeat_interval: 30s
config_sync_interval: 60s
```

**What each setting controls:**

- `agent.id` — Unique identifier for this agent instance. The control plane uses this for registration and config assignment.
- `pop_name` — Identifies which network vantage point this agent represents. In production, this would be something like `us-east-1-aws` or `tokyo-gcp`.
- `transport.backend: nats` — Tells the agent to publish results via NATS JetStream. The alternative is `kafka` for production-scale deployments (50+ agents).
- `buffer.max_size` — How many test results to buffer locally when NATS is unreachable. The agent never loses data — it queues results on disk and replays them when the connection recovers.
- `heartbeat_interval` — How often the agent tells the control plane "I'm alive." Heartbeats continue even if test execution is failing, so the control plane always knows whether an agent is running.

Run the agent:

```bash
./bin/agent --config agent.yaml
```

**Note:** The agent won't run any tests yet because no canary types are implemented (those come in M3–M7). It will, however, start its lifecycle: connect to NATS, begin heartbeats, and attempt config sync. This validates the full agent → NATS pipeline.

## Step 6: Verify the Data Pipeline

With the agent running, verify each link in the chain:

1. **NATS receiving messages:** [http://localhost:8222/jsz](http://localhost:8222/jsz) — Look for the `NETVANTAGE` stream and its message count.

2. **Prometheus scraping:** [http://localhost:9090/targets](http://localhost:9090/targets) — All targets should show "UP" with a recent last scrape time.

3. **Grafana rendering:** [http://localhost:3000](http://localhost:3000) — The home dashboard should show service connectivity status. Once canaries are implemented (M3+), data will populate the monitoring dashboards.

## Stopping and Cleaning Up

```bash
task dev-down    # Stop services, keep data (volumes survive)
task dev-clean   # Stop services AND delete all volumes (fresh start)
```

**When to use which:** `dev-down` for pausing work — your Prometheus history, Grafana settings, and PostgreSQL data survive. `dev-clean` when you want a completely fresh environment (useful after config changes or when troubleshooting).

## What You've Learned

You now have a working instance of every infrastructure component NetVantage depends on. You've seen how the pieces connect: agents → NATS → processor → Prometheus → Grafana, with Alertmanager handling notifications and Routinator providing RPKI validation.

## Next Steps

**[BGP Monitoring Demo](quickstart-bgp.md)** — Set up prefix monitoring, hijack detection, and RPKI validation. This is NetVantage's primary differentiator and doesn't require any Go agents — just the Python BGP analyzer and the infrastructure stack you just started.

**[Architecture Deep Dive](ARCHITECTURE.md)** — Understand the technical decisions behind each component, the transport abstraction layer, and agent resilience patterns.
