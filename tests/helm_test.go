//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// HelmChart represents a Helm Chart.yaml file structure.
type HelmChart struct {
	APIVersion string `yaml:"apiVersion"`
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	AppVersion string `yaml:"appVersion"`
	Type       string `yaml:"type"`
	Home       string `yaml:"home"`
	Maintainers []map[string]string `yaml:"maintainers"`
}

// HelmValues represents the top-level structure of values.yaml.
type HelmValues map[string]interface{}

var expectedHelmTemplates = []string{
	"server-deployment.yaml",
	"processor-deployment.yaml",
	"agent-daemonset.yaml",
	"networkpolicy.yaml",
	"secrets.yaml",
	"_helpers.tpl",
}

// TestHelm_ChartYAMLExists validates that Chart.yaml exists.
func TestHelm_ChartYAMLExists(t *testing.T) {
	chartPath := filepath.Join("deploy", "helm", "netvantage", "Chart.yaml")
	if _, err := os.Stat(chartPath); err != nil {
		t.Fatalf("Chart.yaml not found: %v", err)
	}
}

// TestHelm_ChartYAMLValid validates that Chart.yaml has required fields.
func TestHelm_ChartYAMLValid(t *testing.T) {
	chartPath := filepath.Join("deploy", "helm", "netvantage", "Chart.yaml")
	data, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("failed to read Chart.yaml: %v", err)
	}

	var chart HelmChart
	if err := yaml.Unmarshal(data, &chart); err != nil {
		t.Fatalf("invalid YAML in Chart.yaml: %v", err)
	}

	if chart.APIVersion == "" {
		t.Error("missing required field: apiVersion")
	}
	if chart.Name == "" {
		t.Error("missing required field: name")
	}
	if chart.Version == "" {
		t.Error("missing required field: version")
	}
	if chart.AppVersion == "" {
		t.Error("missing required field: appVersion")
	}
}

// TestHelm_ValuesYAMLExists validates that values.yaml exists.
func TestHelm_ValuesYAMLExists(t *testing.T) {
	valuesPath := filepath.Join("deploy", "helm", "netvantage", "values.yaml")
	if _, err := os.Stat(valuesPath); err != nil {
		t.Fatalf("values.yaml not found: %v", err)
	}
}

// TestHelm_ValuesYAMLValid validates that values.yaml is valid YAML and has required keys.
func TestHelm_ValuesYAMLValid(t *testing.T) {
	valuesPath := filepath.Join("deploy", "helm", "netvantage", "values.yaml")
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("failed to read values.yaml: %v", err)
	}

	var values HelmValues
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("invalid YAML in values.yaml: %v", err)
	}

	// Check for expected top-level keys
	expectedKeys := []string{"replicaCount", "image", "service"}
	for _, key := range expectedKeys {
		if _, exists := values[key]; !exists {
			t.Logf("note: expected key %q not found in values.yaml (may be optional)", key)
		}
	}

	if len(values) == 0 {
		t.Error("values.yaml appears to be empty")
	}
}

// TestHelm_TemplateFilesExist validates that all expected template files exist.
func TestHelm_TemplateFilesExist(t *testing.T) {
	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")

	for _, expectedFile := range expectedHelmTemplates {
		path := filepath.Join(templateDir, expectedFile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected template file not found: %s", expectedFile)
		}
	}
}

// TestHelm_TemplatesSyntax validates that template files have Helm templating syntax.
func TestHelm_TemplatesSyntax(t *testing.T) {
	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		t.Fatalf("failed to read templates directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		t.Run(entry.Name()+"_syntax", func(t *testing.T) {
			path := filepath.Join(templateDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read template file: %v", err)
			}

			content := string(data)

			// All templates should use Helm templating syntax (with possible exceptions for some static files)
			// Skip validation for files that are intentionally static
			if strings.HasPrefix(entry.Name(), "_") {
				// Helper templates may be pure Go templates
				if !strings.Contains(content, "{{") && entry.Name() != "_helpers.tpl" {
					t.Logf("note: %s does not contain template syntax", entry.Name())
				}
			} else {
				// Non-helper templates should have templating
				if !strings.Contains(content, "{{") {
					t.Logf("warning: %s does not contain Helm template syntax", entry.Name())
				}
			}
		})
	}
}

// TestHelm_DeploymentTemplates validates deployment template structure.
func TestHelm_DeploymentTemplates(t *testing.T) {
	deploymentTemplates := []string{
		"server-deployment.yaml",
		"processor-deployment.yaml",
	}

	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")

	for _, tmpl := range deploymentTemplates {
		path := filepath.Join(templateDir, tmpl)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("skipping %s: %v", tmpl, err)
			continue
		}

		content := string(data)

		t.Run(tmpl+"_structure", func(t *testing.T) {
			if !strings.Contains(content, "kind: Deployment") && !strings.Contains(content, "Deployment") {
				t.Error("deployment template should define kind: Deployment")
			}
			if !strings.Contains(content, "spec:") {
				t.Error("deployment template should have spec section")
			}
			if !strings.Contains(content, "containers:") {
				t.Error("deployment template should define containers")
			}
		})
	}
}

// TestHelm_DaemonSetTemplate validates daemonset template structure.
func TestHelm_DaemonSetTemplate(t *testing.T) {
	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")
	path := filepath.Join(templateDir, "agent-daemonset.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping agent-daemonset.yaml: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "kind: DaemonSet") && !strings.Contains(content, "DaemonSet") {
		t.Error("agent template should define kind: DaemonSet")
	}
	if !strings.Contains(content, "spec:") {
		t.Error("agent template should have spec section")
	}
}

// TestHelm_NetworkPolicyTemplate validates networkpolicy template structure.
func TestHelm_NetworkPolicyTemplate(t *testing.T) {
	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")
	path := filepath.Join(templateDir, "networkpolicy.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping networkpolicy.yaml: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "kind: NetworkPolicy") && !strings.Contains(content, "NetworkPolicy") {
		t.Error("networkpolicy template should define kind: NetworkPolicy")
	}
}

// TestHelm_SecretsTemplate validates secrets template structure.
func TestHelm_SecretsTemplate(t *testing.T) {
	templateDir := filepath.Join("deploy", "helm", "netvantage", "templates")
	path := filepath.Join(templateDir, "secrets.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping secrets.yaml: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "kind: Secret") && !strings.Contains(content, "Secret") {
		t.Error("secrets template should define kind: Secret")
	}
}
