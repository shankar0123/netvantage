//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// DashboardValidator contains the expected fields of a Grafana dashboard JSON.
type DashboardValidator struct {
	Title         string `json:"title"`
	UID           string `json:"uid"`
	SchemaVersion int    `json:"schemaVersion"`
	Panels        []struct {
		Title  string `json:"title"`
		Type   string `json:"type"`
		GridPos struct {
			H int `json:"h"`
			W int `json:"w"`
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"gridPos"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
	Tags []string `json:"tags"`
}

var expectedDashboards = []string{
	"home",
	"bgp-events",
	"ping-overview",
	"dns-overview",
	"http-overview",
	"traceroute-overview",
	"platform-health",
	"global-map",
	"target-drilldown",
	"pop-comparison",
}

// TestDashboards_ValidJSON validates that all dashboard files are valid JSON.
func TestDashboards_ValidJSON(t *testing.T) {
	dashboardDir := filepath.Join("grafana", "dashboards")
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("failed to read dashboards directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(dashboardDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read dashboard file: %v", err)
			}

			var dash DashboardValidator
			if err := json.Unmarshal(data, &dash); err != nil {
				t.Errorf("invalid JSON: %v", err)
			}
		})
	}
}

// TestDashboards_RequiredFields validates that all dashboards have required fields.
func TestDashboards_RequiredFields(t *testing.T) {
	dashboardDir := filepath.Join("grafana", "dashboards")
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("failed to read dashboards directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		t.Run(entry.Name()+"_required_fields", func(t *testing.T) {
			path := filepath.Join(dashboardDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read dashboard file: %v", err)
			}

			var dash DashboardValidator
			if err := json.Unmarshal(data, &dash); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			if dash.Title == "" {
				t.Error("missing required field: title")
			}
			if dash.UID == "" {
				t.Error("missing required field: uid")
			}
			if dash.SchemaVersion == 0 {
				t.Error("missing or zero schemaVersion")
			}
			if dash.Panels == nil {
				t.Error("missing required field: panels (must be array)")
			}

			// Check for netvantage tag
			hasNetvantageTag := false
			for _, tag := range dash.Tags {
				if tag == "netvantage" {
					hasNetvantageTag = true
					break
				}
			}
			if !hasNetvantageTag {
				t.Error("missing required tag: netvantage")
			}
		})
	}
}

// TestDashboards_PanelValidation validates that all panels have required fields.
func TestDashboards_PanelValidation(t *testing.T) {
	dashboardDir := filepath.Join("grafana", "dashboards")
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("failed to read dashboards directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		t.Run(entry.Name()+"_panels", func(t *testing.T) {
			path := filepath.Join(dashboardDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read dashboard file: %v", err)
			}

			var dash DashboardValidator
			if err := json.Unmarshal(data, &dash); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			for i, panel := range dash.Panels {
				if panel.Title == "" {
					t.Errorf("panel %d: missing title", i)
				}
				if panel.Type == "" {
					t.Errorf("panel %d: missing type", i)
				}
				if panel.GridPos.H == 0 {
					t.Errorf("panel %d: invalid or missing gridPos.h", i)
				}
				if panel.GridPos.W == 0 {
					t.Errorf("panel %d: invalid or missing gridPos.w", i)
				}

				// Validate targets if present
				for j, target := range panel.Targets {
					if target.Expr == "" {
						t.Errorf("panel %d target %d: missing or empty expr field", i, j)
					}
				}
			}
		})
	}
}

// TestDashboards_NoDuplicateUIDs validates that no two dashboards share the same UID.
func TestDashboards_NoDuplicateUIDs(t *testing.T) {
	dashboardDir := filepath.Join("grafana", "dashboards")
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("failed to read dashboards directory: %v", err)
	}

	uids := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dashboardDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read dashboard file: %v", err)
		}

		var dash DashboardValidator
		if err := json.Unmarshal(data, &dash); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		if dash.UID == "" {
			continue
		}

		if existing, found := uids[dash.UID]; found {
			t.Errorf("duplicate UID %q in %s and %s", dash.UID, existing, entry.Name())
		}
		uids[dash.UID] = entry.Name()
	}
}

// TestDashboards_ExpectedDashboardsExist validates that all expected dashboards exist.
func TestDashboards_ExpectedDashboardsExist(t *testing.T) {
	dashboardDir := filepath.Join("grafana", "dashboards")
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("failed to read dashboards directory: %v", err)
	}

	foundDashboards := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".json" {
			// Strip .json extension
			baseName := name[:len(name)-5]
			foundDashboards[baseName] = true
		}
	}

	for _, expected := range expectedDashboards {
		if !foundDashboards[expected] {
			t.Errorf("expected dashboard not found: %s.json", expected)
		}
	}
}
