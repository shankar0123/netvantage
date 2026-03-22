//go:build e2e

// Package e2e contains end-to-end tests that exercise the full NetVantage
// pipeline with real infrastructure (NATS JetStream, PostgreSQL).
//
// These tests require Docker and are NOT run in the normal `go test ./...`
// flow. They are gated behind the `e2e` build tag and require testcontainers.
//
// Run with:
//
//	go test -v -tags=e2e -timeout=10m ./tests/e2e/...
package e2e

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Shared test infrastructure — started once in TestMain, reused by all tests.
// This avoids spinning up a new container per test (each takes ~10s in CI).
var (
	sharedNATSURL    string
	sharedNATSContainer testcontainers.Container

	sharedPGConnStr    string
	sharedPGContainer  testcontainers.Container
)

// TestMain starts shared infrastructure containers once before any test runs,
// and tears them down after all tests complete. This reduces E2E suite time
// from ~80s (per-test containers) to ~20s (shared containers).
func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var code int
	defer func() { os.Exit(code) }()

	// Start NATS.
	var err error
	sharedNATSContainer, sharedNATSURL, err = startNATSContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start NATS: %v\n", err)
		code = 1
		return
	}
	defer sharedNATSContainer.Terminate(ctx) //nolint:errcheck

	// Start PostgreSQL.
	sharedPGContainer, sharedPGConnStr, err = startPostgresContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start PostgreSQL: %v\n", err)
		code = 1
		return
	}
	defer sharedPGContainer.Terminate(ctx) //nolint:errcheck

	code = m.Run()
}

// testLogger returns a silent slog.Logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startNATSContainer spins up a NATS server with JetStream enabled.
func startNATSContainer(ctx context.Context) (testcontainers.Container, string, error) {
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
		return nil, "", fmt.Errorf("start NATS container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("get NATS host: %w", err)
	}

	port, err := container.MappedPort(ctx, "4222")
	if err != nil {
		return nil, "", fmt.Errorf("get NATS port: %w", err)
	}

	return container, fmt.Sprintf("nats://%s:%s", host, port.Port()), nil
}

// startPostgresContainer spins up a PostgreSQL instance.
func startPostgresContainer(ctx context.Context) (testcontainers.Container, string, error) {
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
		return nil, "", fmt.Errorf("start PostgreSQL container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("get PostgreSQL host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		return nil, "", fmt.Errorf("get PostgreSQL port: %w", err)
	}

	connStr := fmt.Sprintf("postgres://netvantage:netvantage@%s:%s/netvantage?sslmode=disable", host, port.Port())
	return container, connStr, nil
}

// freePort finds and returns a free localhost address (host:port).
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// waitForHTTP polls a URL until it returns 200 or the timeout expires.
func waitForHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", url)
}
