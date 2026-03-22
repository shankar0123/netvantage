//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// AlertRuleGroup represents a Prometheus alert rule group.
type AlertRuleGroup struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert       string `yaml:"alert"`
			Expr        string `yaml:"expr"`
			For         string `yaml:"for"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

var expectedAlertFiles = []string{
	"bgp_alerts",
	"ping_alerts",
	"dns_alerts",
	"http_alerts",
	"traceroute_alerts",
	"platform_alerts",
	"correlation_alerts",
}

// TestAlertRules_ValidYAML validates that all alert rule files are valid YAML.
func TestAlertRules_ValidYAML(t *testing.T) {
	rulesDir := filepath.Join("prometheus", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("failed to read rules directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(rulesDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read alert rules file: %v", err)
			}

			var rules AlertRuleGroup
			if err := yaml.Unmarshal(data, &rules); err != nil {
				t.Errorf("invalid YAML: %v", err)
			}
		})
	}
}

// TestAlertRules_RequiredStructure validates that alert rule files have required structure.
func TestAlertRules_RequiredStructure(t *testing.T) {
	rulesDir := filepath.Join("prometheus", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("failed to read rules directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		t.Run(entry.Name()+"_structure", func(t *testing.T) {
			path := filepath.Join(rulesDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read alert rules file: %v", err)
			}

			var rules AlertRuleGroup
			if err := yaml.Unmarshal(data, &rules); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}

			if len(rules.Groups) == 0 {
				t.Error("no groups defined in rules file")
			}

			for i, group := range rules.Groups {
				if group.Name == "" {
					t.Errorf("group %d: missing name", i)
				}
				if len(group.Rules) == 0 {
					t.Errorf("group %d (%s): no rules defined", i, group.Name)
				}
			}
		})
	}
}

// TestAlertRules_RuleFields validates that all rules have required fields.
func TestAlertRules_RuleFields(t *testing.T) {
	rulesDir := filepath.Join("prometheus", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("failed to read rules directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		t.Run(entry.Name()+"_rule_fields", func(t *testing.T) {
			path := filepath.Join(rulesDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read alert rules file: %v", err)
			}

			var rules AlertRuleGroup
			if err := yaml.Unmarshal(data, &rules); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}

			for gi, group := range rules.Groups {
				for ri, rule := range group.Rules {
					if rule.Alert == "" {
						t.Errorf("group %d (%s) rule %d: missing alert name", gi, group.Name, ri)
					}
					if rule.Expr == "" {
						t.Errorf("group %d (%s) rule %d: missing expr", gi, group.Name, ri)
					}
					if rule.For == "" {
						t.Errorf("group %d (%s) rule %d: missing for duration", gi, group.Name, ri)
					}
					if rule.Labels == nil || rule.Labels["severity"] == "" {
						t.Errorf("group %d (%s) rule %d: missing labels.severity", gi, group.Name, ri)
					}
					if rule.Annotations == nil || rule.Annotations["summary"] == "" {
						t.Errorf("group %d (%s) rule %d: missing annotations.summary", gi, group.Name, ri)
					}
				}
			}
		})
	}
}

// TestAlertRules_NamingConvention validates that alert names follow the NetVantage naming convention.
func TestAlertRules_NamingConvention(t *testing.T) {
	rulesDir := filepath.Join("prometheus", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("failed to read rules directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		t.Run(entry.Name()+"_naming", func(t *testing.T) {
			path := filepath.Join(rulesDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read alert rules file: %v", err)
			}

			var rules AlertRuleGroup
			if err := yaml.Unmarshal(data, &rules); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}

			for gi, group := range rules.Groups {
				for ri, rule := range group.Rules {
					if !strings.HasPrefix(rule.Alert, "NetVantage") {
						t.Errorf("group %d (%s) rule %d: alert name %q should start with 'NetVantage'",
							gi, group.Name, ri, rule.Alert)
					}
				}
			}
		})
	}
}

// TestAlertRules_ExpectedFilesExist validates that all expected alert rule files exist.
func TestAlertRules_ExpectedFilesExist(t *testing.T) {
	rulesDir := filepath.Join("prometheus", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("failed to read rules directory: %v", err)
	}

	foundFiles := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".yml" || filepath.Ext(name) == ".yaml" {
			// Strip extension
			baseName := strings.TrimSuffix(name, filepath.Ext(name))
			foundFiles[baseName] = true
		}
	}

	for _, expected := range expectedAlertFiles {
		if !foundFiles[expected] {
			t.Errorf("expected alert rules file not found: %s.yml", expected)
		}
	}
}
