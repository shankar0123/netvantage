//go:build e2e

// Package e2e contains end-to-end tests that exercise the full NetVantage
// pipeline with real infrastructure (NATS JetStream, PostgreSQL, Prometheus).
//
// These tests require Docker and are NOT run in the normal `go test ./...`
// flow. They are gated behind the `e2e` build tag and require testcontainers.
//
// Run with:
//
//	go test -v -tags=e2e -timeout=5m ./tests/e2e/...
package e2e

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testLogger returns a silent slog.Logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startNATS spins up a NATS server with JetStream enabled via testcontainers.
// Returns the container, the NATS URL, and a cleanup function.
func startNATS(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor:   wait.ForListeningPort("4222/tcp").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start NATS container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get NATS host: %v", err)
	}

	port, err := container.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("failed to get NATS port: %v", err)
	}

	natsURL := fmt.Sprintf("nats://%s:%s", host, port.Port())
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate NATS container: %v", err)
		}
	})

	return container, natsURL
}

// startPostgres spins up a PostgreSQL instance via testcontainers.
// Returns the container and the connection string.
func startPostgres(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "netvantage",
			"POSTGRES_PASSWORD": "netvantage",
			"POSTGRES_DB":       "netvantage",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start PostgreSQL container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get PostgreSQL host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("failed to get PostgreSQL port: %v", err)
	}

	connStr := fmt.Sprintf("postgres://netvantage:netvantage@%s:%s/netvantage?sslmode=disable", host, port.Port())
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate PostgreSQL container: %v", err)
		}
	})

	return container, connStr
}

// startPrometheus spins up a Prometheus instance with remote_write enabled.
// Returns the container and the Prometheus URL.
func startPrometheus(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "prom/prometheus:v2.51.0",
		ExposedPorts: []string{"9090/tcp"},
		Cmd: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.retention.time=1h",
			"--web.enable-remote-write-receiver",
			"--web.enable-lifecycle",
		},
		WaitingFor: wait.ForHTTP("/-/ready").WithPort("9090/tcp").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start Prometheus container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get Prometheus host: %v", err)
	}

	port, err := container.MappedPort(ctx, "9090")
	if err != nil {
		t.Fatalf("failed to get Prometheus port: %v", err)
	}

	promURL := fmt.Sprintf("http://%s:%s", host, port.Port())
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate Prometheus container: %v", err)
		}
	})

	return container, promURL
}

// queryPrometheus queries a Prometheus instance and returns whether the query
// returned any results. Used to verify metrics were written correctly.
func queryPrometheus(promURL, query string) (bool, string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/query?query=%s", promURL, query))
	if err != nil {
		return false, "", fmt.Errorf("prometheus query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("failed to read prometheus response: %w", err)
	}

	bodyStr := string(body)
	hasResults := strings.Contains(bodyStr, `"result":[{`)
	return hasResults, bodyStr, nil
}
