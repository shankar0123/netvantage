//go:build e2e

package e2e

// BGP Analyzer E2E tests are intentionally not included here.
//
// The BGP Analyzer is a Python service with its own CI job (lint → test → build)
// that runs pytest with recorded MRT fixtures and builds the Docker image.
// Duplicating that work in the Go E2E suite adds ~2 minutes of Docker build time
// with no additional coverage.
//
// If BGP Analyzer integration with the Go pipeline is needed in the future
// (e.g., verifying Prometheus metrics written by the analyzer), add tests here
// that use testcontainers to spin up both the BGP Analyzer container and a
// Prometheus instance.
