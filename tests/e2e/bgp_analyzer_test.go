//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestBGPAnalyzer_DockerBuild verifies that the BGP Analyzer Docker image
// builds successfully. This catches dependency issues, Dockerfile errors,
// and Python packaging problems that unit tests miss.
func TestBGPAnalyzer_DockerBuild(t *testing.T) {
	bgpDir := filepath.Join("..", "..", "bgp-analyzer")
	if _, err := os.Stat(filepath.Join(bgpDir, "Dockerfile")); os.IsNotExist(err) {
		t.Skip("bgp-analyzer/Dockerfile not found, skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", "netvantage-bgp-analyzer:e2e-test", bgpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("BGP Analyzer Docker build failed: %v", err)
	}
}

// TestBGPAnalyzer_PythonTests runs the BGP Analyzer Python test suite inside
// the Docker image. This verifies that the containerized Python environment
// matches what's expected — catching missing dependencies, path issues, and
// fixture access problems.
func TestBGPAnalyzer_PythonTests(t *testing.T) {
	bgpDir := filepath.Join("..", "..", "bgp-analyzer")
	if _, err := os.Stat(filepath.Join(bgpDir, "Dockerfile")); os.IsNotExist(err) {
		t.Skip("bgp-analyzer/Dockerfile not found, skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Run pytest inside the container.
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/app", bgpDir),
		"-w", "/app",
		"python:3.12-slim",
		"sh", "-c", "pip install -e '.[dev]' -q && pytest --tb=short -v",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("BGP Analyzer Python tests failed inside container: %v", err)
	}
}
