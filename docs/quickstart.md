# Quick Start Guide

Get NetVantage running locally in under 5 minutes.

## Prerequisites

- **Docker** and **Docker Compose** (v2)
- **Go 1.22+** — for building the agent, server, and processor
- **Task** — install with `go install github.com/go-task/task/v3/cmd/task@latest` or `brew install go-task`

## 1. Clone and Install Dependencies

```bash
git clone https://github.com/<your-username>/netvantage.git
cd netvantage
go mod tidy
```

## 2. Start the Infrastructure Stack

```bash
task dev-up
```

This starts six services:

| Service | Port | Purpose |
|---|---|---|
| NATS JetStream | 4222, 8222 | Message transport + monitoring UI |
| Prometheus | 9090 | Metrics storage and alerting |
| Grafana | 3000 | Dashboards (login: admin/admin) |
| PostgreSQL | 5432 | Control plane database |
| Alertmanager | 9093 | Alert routing |
| Routinator | 3323, 9556 | RPKI validation cache |

Verify everything is healthy:

```bash
task dev-logs
```

## 3. Open Grafana

Navigate to [http://localhost:3000](http://localhost:3000) and log in with `admin` / `admin`. You'll see the NetVantage home dashboard with links to all available dashboards.

## 4. Build the Services

```bash
# Build Go binaries
task build-agent
task build-server
task build-processor

# Build the BGP Analyzer container
task build-bgp
```

## 5. Run the Agent (Development Mode)

The agent needs a NATS connection to publish results:

```bash
./bin/agent --config agent.yaml
```

A minimal `agent.yaml` for local development:

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

## 6. Verify the Pipeline

Once the agent is running:

1. Check NATS is receiving messages: [http://localhost:8222/jsz](http://localhost:8222/jsz)
2. Check Prometheus is scraping: [http://localhost:9090/targets](http://localhost:9090/targets)
3. Check Grafana dashboards are populating: [http://localhost:3000](http://localhost:3000)

## Stopping and Cleaning Up

```bash
task dev-down    # Stop services, keep data
task dev-clean   # Stop services and destroy all volumes
```

## Next Steps

- **BGP Monitoring:** See [docs/quickstart-bgp.md](quickstart-bgp.md) (available after M2) for configuring prefix monitoring, hijack detection, and RPKI validation.
- **Architecture:** Read [docs/ARCHITECTURE.md](ARCHITECTURE.md) for the full system design.
- **Contributing:** See [docs/CONTRIBUTING.md](CONTRIBUTING.md) for development conventions and how to add new canary types.
