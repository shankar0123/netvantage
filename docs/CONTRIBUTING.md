# Contributing to NetVantage

## Development Setup

### Prerequisites

- Go 1.22+
- Python 3.12+
- Docker and Docker Compose
- [Task](https://taskfile.dev) — install with `go install github.com/go-task/task/v3/cmd/task@latest` or `brew install go-task`

### Getting Started

```bash
git clone https://github.com/<your-username>/netvantage.git
cd netvantage

# Install Go dependencies
go mod tidy

# Start the dev infrastructure stack
task dev-up

# Run all tests
task test
task test-bgp

# Run linters
task lint-go
task lint-python
```

### Dev Stack Services

`task dev-up` starts NATS JetStream, Prometheus, Grafana, PostgreSQL, Alertmanager, and Routinator. All data is persisted in Docker volumes. Use `task dev-down` to stop and `task dev-clean` to stop and destroy volumes.

## Project Structure

```
cmd/                    # Service entry points
  agent/                #   Canary agent
  server/               #   Control plane API
  processor/            #   Metrics processor
internal/               # Private packages
  agent/                #   Agent lifecycle, config, buffer
    canary/             #   Canary interface + implementations
      ping/             #     ICMP ping (M3)
      dns/              #     DNS resolution (M4)
      http/             #     HTTP/S monitoring (M6)
      traceroute/       #     Traceroute mapping (M7)
  transport/            #   Transport abstraction layer
    nats/               #     NATS JetStream backend
    kafka/              #     Kafka backend (M9)
    memory/             #     In-memory (tests)
  processor/            #   Metrics processing logic
  server/               #   Control plane API logic
  domain/               #   Shared domain models
bgp-analyzer/           # Python BGP analysis service
  src/netvantage_bgp/   #   Package source
  tests/                #   Test suite
grafana/                # Dashboard provisioning
prometheus/             # Scrape configs and alert rules
alertmanager/           # Alert routing config
docs/                   # Documentation
deploy/                 # Helm charts, Terraform, Ansible (M9+)
migrations/             # PostgreSQL migrations
proto/                  # Protobuf schemas (M9)
```

## Development Workflow

### Branching

Work on feature branches off `main`. Branch naming: `<type>/<short-description>` (e.g., `feat/ping-canary`, `fix/nats-reconnect`).

### Commits

Use conventional commits:

- `feat:` — new feature
- `fix:` — bug fix
- `docs:` — documentation only
- `chore:` — build, CI, dependency updates
- `test:` — adding or updating tests
- `ci:` — CI pipeline changes

Reference milestone and requirement IDs where applicable:

```
feat(bgp): implement hijack detection [M2, BGP-04]
fix(agent): recover from canary panic without dropping heartbeat [M3]
```

### Pull Requests

PRs should target `main` and include a clear description of what changed and why. Reference the milestone (M1–M10) in the PR title or body. CI must pass before merge.

## Code Conventions

### Go

- **Formatting:** `gofmt` (enforced by CI)
- **Linting:** `golangci-lint` with the project's config
- **Logging:** `slog` with structured fields. No `fmt.Printf` in production code.
- **Error handling:** Return domain errors from services. API handlers map to HTTP status codes. Canary failures return `Result{Success: false, Error: "..."}` — never panic.
- **Testing:** Table-driven tests for canary logic. `httptest` for handler tests. `testcontainers-go` for integration tests against real NATS and PostgreSQL.
- **HTTP:** stdlib `net/http` with `chi` router. No heavy frameworks.
- **Database:** `database/sql` + `pgx`. Raw SQL. No ORM. Migrations are idempotent (`IF NOT EXISTS`, `ON CONFLICT`).

### Python (BGP Analyzer)

- **Formatting:** `black` + `isort`
- **Linting:** `ruff`
- **Logging:** `structlog` with structured fields
- **Testing:** `pytest` with recorded MRT data fixtures

### Prometheus Metrics

- All metrics use the `netvantage_` prefix
- Alert rule names: `NetVantage<Type><Condition>` (e.g., `NetVantagePingHighLatency`)
- Transport topics: `netvantage.<test_type>.results`

### Dashboards

Grafana dashboards are provisioned as code — JSON files in `grafana/dashboards/`. Never create dashboards manually through the Grafana UI. Validate dashboard JSON in CI with `task dashboards-validate`.

## Adding a New Canary Type

1. Create a package under `internal/agent/canary/<type>/`.
2. Implement the `Canary` interface:

```go
type Canary interface {
    Type() string
    Execute(ctx context.Context, test TestDefinition) (*Result, error)
    Validate(config json.RawMessage) error
}
```

3. Register the canary in `cmd/agent/main.go` with `agent.RegisterCanary()`.
4. Add a result handler in `internal/processor/` to map results to Prometheus metrics.
5. Create a Grafana dashboard JSON in `grafana/dashboards/`.
6. Add Prometheus alert rules in `prometheus/rules/`.
7. Write table-driven unit tests and at least one integration test.
8. Document the canary in `docs/`.

Every canary ships with its dashboard, alert rules, and documentation — or it doesn't ship.

## Running Tests

```bash
task test              # Go unit tests
task test-integration  # Go integration tests (requires Docker)
task test-bgp          # Python BGP analyzer tests
task lint-go           # Go linting
task lint-python       # Python linting
```

## Questions?

Open an issue for bugs, feature requests, or questions about the architecture.
