# Contributing to NetVantage

This guide covers the development workflow, codebase conventions, and how to extend the platform. Each section explains not just the rules but the reasoning behind them — understanding the "why" helps you make good decisions in cases the rules don't cover.

## Development Setup

### Prerequisites

- **Go 1.22+** — The agent, server, and processor are Go services. Go's toolchain includes formatting, vetting, and testing out of the box.
- **Python 3.12+** — The BGP analyzer is a separate Python service with its own dependency management.
- **Docker and Docker Compose** — Runs the infrastructure stack (NATS, Prometheus, Grafana, PostgreSQL, Alertmanager, Routinator). Also used for integration tests.
- **[Task](https://taskfile.dev)** — Install with `go install github.com/go-task/task/v3/cmd/task@latest` or `brew install go-task`. We use Task instead of Make because Taskfile YAML is more readable than Makefile syntax, and Task handles cross-platform differences.

### Getting Started

```bash
git clone https://github.com/shankar0123/netvantage.git
cd netvantage

# Resolve Go dependencies (generates go.sum)
go mod tidy

# Start the infrastructure stack
task dev-up

# Run all tests
task test       # Go unit tests
task test-bgp   # Python BGP tests

# Run linters
task lint-go      # golangci-lint
task lint-python  # ruff
```

### Dev Stack Services

`task dev-up` starts NATS JetStream, Prometheus, Grafana, PostgreSQL, Alertmanager, and Routinator. All data is persisted in Docker volumes — it survives `task dev-down`. Use `task dev-clean` to destroy volumes and start fresh.

**Why persistent volumes by default?** If you're working on the BGP analyzer and restart the stack, you don't want to wait 5 minutes for Routinator to re-sync RPKI data. Persistent volumes keep state between restarts. `dev-clean` is there for when you need a truly fresh environment.

## Project Structure

```
cmd/                    # Service entry points
  agent/                #   Canary agent — runs at each POP
  server/               #   Control plane API — centralized management
  processor/            #   Metrics processor — transforms results into Prometheus metrics
internal/               # Private packages (not importable by external code)
  agent/                #   Agent lifecycle, config, buffer
    canary/             #   Canary interface + implementations
      ping/             #     ICMP ping (M3)
      dns/              #     DNS resolution (M4)
      http/             #     HTTP/S monitoring (M6)
      traceroute/       #     Traceroute mapping (M7)
  transport/            #   Transport abstraction layer
    nats/               #     NATS JetStream backend (default)
    kafka/              #     Kafka backend (production, M9)
    memory/             #     In-memory backend (unit tests)
  processor/            #   Metrics processing logic
  server/               #   Control plane API logic
  domain/               #   Shared domain models
bgp-analyzer/           # Python BGP analysis service (independent lifecycle)
  src/netvantage_bgp/   #   Package source
  tests/                #   Test suite (mocked, no live BGP data)
grafana/                # Dashboard-as-code (provisioned JSON files)
prometheus/             # Scrape configs and alert rules
alertmanager/           # Alert routing config
docs/                   # All documentation (you're reading it)
deploy/                 # Helm charts, Terraform, Ansible (M9+)
migrations/             # PostgreSQL migrations (numbered, idempotent)
proto/                  # Protobuf schemas (M9 — JSON until then)
```

**Why `internal/`?** Go's `internal` package convention prevents external packages from importing these modules. This is deliberate — it gives us freedom to restructure internals without breaking external consumers. The only stable API surface is the transport interface and the canary interface.

**Why is `bgp-analyzer/` at the top level?** It's a completely separate service with its own language, dependencies, Dockerfile, and CI job. Nesting it under `internal/` or `cmd/` would imply it's part of the Go module, which it's not. It has its own `pyproject.toml` and its own release lifecycle.

## Development Workflow

### Branching

Work on feature branches off `main`. Branch naming: `<type>/<short-description>`.

Examples: `feat/ping-canary`, `fix/nats-reconnect`, `docs/architecture-diagrams`.

**Why conventional branch names?** Consistency makes it easy to scan a branch list and understand what's in flight. The type prefix matches our commit convention.

### Commits

Use [conventional commits](https://www.conventionalcommits.org/):

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
docs: add BGP monitoring quickstart [M2]
```

**Why conventional commits?** They make the git log scannable and enable automated changelog generation. The milestone reference (e.g., `[M2, BGP-04]`) ties every change to the roadmap, making it easy to verify what shipped in each milestone.

### Pull Requests

PRs should target `main` and include a clear description of what changed and why. Reference the milestone (M1–M10) in the PR title or body. CI must pass before merge.

**Why "what and why" in PRs?** "What" is visible in the diff. "Why" is the context that helps reviewers make good decisions and helps future developers understand the intent. A PR that says "add timeout to NATS reconnect" is less useful than "add timeout to NATS reconnect because the default infinite retry blocks agent shutdown."

## Code Conventions

### Go

**Formatting:** `gofmt`. Non-negotiable, enforced by CI. **Why?** Go's community settled this debate: one format, no arguments, automated. Zero time spent on formatting discussions.

**Linting:** `golangci-lint` with the project's config. **Why golangci-lint specifically?** It runs dozens of linters in parallel and caches results. It catches bugs, performance issues, and code smells that `go vet` alone misses.

**Logging:** `slog` with structured fields. No `fmt.Printf` in production code. **Why structured logging?** When you're debugging an issue across 50 agents, you need to filter logs by agent_id, prefix, canary_type, etc. `fmt.Printf("error: %v", err)` is unusable at scale. `slog.Error("publish_failed", "agent_id", id, "error", err)` is searchable and parseable.

**Error handling:** Return domain errors from services. API handlers map to HTTP status codes. Canary failures return `Result{Success: false, Error: "..."}` — never panic. **Why no panics?** A panic in one canary should not crash the agent and kill monitoring for all other canary types. The agent wraps each test execution in `defer/recover` to ensure fault isolation.

**Testing:** Table-driven tests for canary logic. `httptest` for handler tests. `testcontainers-go` for integration tests against real NATS and PostgreSQL. **Why table-driven tests?** They make it trivial to add new test cases — just add a row to the table. They also make the test matrix visible at a glance.

**HTTP:** stdlib `net/http` with `chi` router. No heavy frameworks. **Why not Gin, Echo, Fiber?** The Go standard library's HTTP server is production-grade. `chi` adds URL parameters and middleware without pulling in a large dependency tree. Heavy frameworks add abstraction layers that hide behavior — the opposite of what we want for a system where HTTP semantics matter.

**Database:** `database/sql` + `pgx`. Raw SQL. No ORM. Migrations are idempotent (`IF NOT EXISTS`, `ON CONFLICT`). **Why raw SQL?** See the [Architecture doc](ARCHITECTURE.md) — ORMs hide query behavior that matters for performance and correctness.

### Python (BGP Analyzer)

**Formatting:** `ruff format` (replaces `black` + `isort` in a single tool). **Linting:** `ruff check`. **Why ruff?** It's dramatically faster than running black, isort, flake8, and pylint separately, and it covers all of them.

**Logging:** `structlog` with structured fields. Same reasoning as Go's `slog` — structured, searchable, parseable.

**Testing:** `pytest` with mock BGP data. Tests never require live pybgpstream or network access. **Why mocks instead of recorded MRT data?** Unit tests should be fast, deterministic, and require no external dependencies. Recorded fixtures are planned for integration tests.

### Prometheus Metrics

- All metrics use the `netvantage_` prefix. **Why?** Prevents collisions with metrics from other systems in the same Prometheus instance.
- Alert rule names: `NetVantage<Type><Condition>` (e.g., `NetVantagePingHighLatency`). **Why PascalCase?** It's the Prometheus community convention for alert names.
- Transport topics: `netvantage.<test_type>.results`. **Why per-type topics?** The metrics processor can subscribe to specific canary types and scale each consumer independently.

### Dashboards

Grafana dashboards are provisioned as code — JSON files in `grafana/dashboards/`. Never create dashboards manually through the Grafana UI. Validate dashboard JSON in CI with `task dashboards-validate`.

**Why not create dashboards in the UI and export them?** Exported dashboards contain auto-generated IDs, UI state, and formatting noise that create diff churn. Hand-authored dashboard JSON is cleaner, more intentional, and easier to review in PRs.

### Diagrams

All diagrams use **Mermaid** format (`.mermaid` files or fenced code blocks in Markdown). No ASCII art, draw.io, Lucidchart, or image-based diagrams.

**Why Mermaid?** It's text-based (diffable, reviewable in PRs), renders natively on GitHub, and doesn't require external tools. Image-based diagrams go stale the moment someone changes the architecture and forgets to update the PNG.

## Adding a New Canary Type

This is the most common extension point. Every canary type follows the same pattern — here's what to build and why each piece matters:

**1. Implement the interface** — Create a package under `internal/agent/canary/<type>/`:

```go
type Canary interface {
    Type() string                                               // Unique identifier (e.g., "ping", "dns")
    Execute(ctx context.Context, test TestDefinition) (*Result, error)  // Run the test
    Validate(config json.RawMessage) error                      // Validate config before execution
}
```

`Validate` exists because bad config should fail fast at startup, not at test execution time 30 minutes later.

**2. Register it** — In `cmd/agent/main.go`, call `agent.RegisterCanary()`. This wires it into the agent's test execution loop with automatic panic recovery and scheduling.

**3. Add a processor handler** — In `internal/processor/`, add a handler that consumes results from the canary's NATS topic and maps them to Prometheus metrics. This is where raw results become time-series data.

**4. Create a dashboard** — JSON file in `grafana/dashboards/`. Include relevant visualizations for the canary's metrics. Every canary should have at minimum: a time-series panel showing the primary metric, a stat panel for current status, and error/failure rate.

**5. Add alert rules** — YAML file in `prometheus/rules/`. Define what conditions are alertable. Think about severity: packet loss > 50% might be critical, while > 10% is a warning.

**6. Write tests** — Table-driven unit tests for the canary logic. At least one integration test that verifies the full pipeline (agent → NATS → processor → Prometheus query).

**7. Document it** — Add a section in docs explaining what the canary measures, how to configure it, and what the dashboard shows.

The rule is simple: **every canary ships with its dashboard, alert rules, and documentation — or it doesn't ship.** This ensures that monitoring capabilities are always observable and actionable from day one.

## Running Tests

```bash
task test              # Go unit tests (fast, no external deps)
task test-integration  # Go integration tests (starts real NATS/Postgres via testcontainers)
task test-bgp          # Python BGP analyzer tests (mocked, no pybgpstream needed)
task lint-go           # Go linting (golangci-lint)
task lint-python       # Python linting (ruff)
task dashboards-validate  # Validate Grafana dashboard JSON syntax
```

**Why separate `test` and `test-integration`?** Unit tests should run in under 5 seconds with zero external dependencies. Integration tests start real containers (NATS, PostgreSQL) and take longer. Developers run unit tests constantly; integration tests run in CI and before merges.

## Questions?

Open an issue for bugs, feature requests, or questions about the architecture. Reference the relevant milestone and component in the title.
