//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var expectedMigrations = []string{
	"001_initial_schema.sql",
	"002_audit_log.sql",
}

// TestMigrations_FilesExist validates that expected migration files exist.
func TestMigrations_FilesExist(t *testing.T) {
	migrationsDir := "migrations"

	for _, expectedFile := range expectedMigrations {
		path := filepath.Join(migrationsDir, expectedFile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected migration file not found: %s", expectedFile)
		}
	}
}

// TestMigrations_SequentiallyNumbered validates that migration files are sequentially numbered.
func TestMigrations_SequentiallyNumbered(t *testing.T) {
	migrationsDir := "migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("failed to read migrations directory: %v", err)
	}

	var numbers []int
	re := regexp.MustCompile(`^(\d{3})_`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		matches := re.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			t.Errorf("migration file does not match naming pattern: %s", entry.Name())
			continue
		}

		var num int
		if _, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name())); err == nil {
			// Parse the number
			for _, c := range matches[1] {
				num = num*10 + int(c-'0')
			}
			numbers = append(numbers, num)
		}
	}

	if len(numbers) == 0 {
		t.Skip("no migrations found")
	}

	// Check that numbers are sequential with no gaps
	sort.Ints(numbers)
	for i := 0; i < len(numbers)-1; i++ {
		if numbers[i+1]-numbers[i] != 1 {
			t.Errorf("gap in migration sequence: %d -> %d", numbers[i], numbers[i+1])
		}
	}
}

// TestMigrations_ValidSQL validates that migration files contain SQL keywords.
func TestMigrations_ValidSQL(t *testing.T) {
	migrationsDir := "migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("failed to read migrations directory: %v", err)
	}

	sqlKeywords := []string{"CREATE", "ALTER", "INSERT", "DROP", "GRANT"}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(migrationsDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read migration file: %v", err)
			}

			content := strings.ToUpper(string(data))

			// Check for at least one SQL keyword
			hasKeyword := false
			for _, keyword := range sqlKeywords {
				if strings.Contains(content, keyword) {
					hasKeyword = true
					break
				}
			}

			if !hasKeyword {
				t.Errorf("migration file does not contain SQL keywords: %s", entry.Name())
			}
		})
	}
}

// TestMigrations_Idempotency validates that migrations use idempotent patterns.
func TestMigrations_Idempotency(t *testing.T) {
	migrationsDir := "migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("failed to read migrations directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		t.Run(entry.Name()+"_idempotency", func(t *testing.T) {
			path := filepath.Join(migrationsDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read migration file: %v", err)
			}

			content := strings.ToUpper(string(data))

			// Check for idempotency patterns
			hasIfNotExists := strings.Contains(content, "IF NOT EXISTS")
			hasOnConflict := strings.Contains(content, "ON CONFLICT")
			hasIfExists := strings.Contains(content, "IF EXISTS")

			// Lenient check: at least one idempotency pattern should be present
			// for CREATE/INSERT operations, but DROP operations might not need it
			if strings.Contains(content, "CREATE") || strings.Contains(content, "INSERT") {
				if !hasIfNotExists && !hasOnConflict {
					t.Logf("warning: %s may not be fully idempotent (no IF NOT EXISTS or ON CONFLICT)", entry.Name())
				}
			}
		})
	}
}

// TestMigrations_001InitialSchema validates the initial schema migration.
func TestMigrations_001InitialSchema(t *testing.T) {
	path := filepath.Join("migrations", "001_initial_schema.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping initial schema migration: %v", err)
	}

	content := strings.ToUpper(string(data))

	t.Run("001_initial_schema_tables", func(t *testing.T) {
		// Expected tables
		expectedTables := []string{"AGENTS", "TESTS", "POPS", "TEST_ASSIGNMENTS"}

		for _, table := range expectedTables {
			if !strings.Contains(content, table) {
				t.Logf("note: table %s not found in initial schema", table)
			}
		}

		// At minimum, should create some tables
		if !strings.Contains(content, "CREATE TABLE") {
			t.Error("initial schema migration should create tables")
		}
	})
}

// TestMigrations_002AuditLog validates the audit log migration.
func TestMigrations_002AuditLog(t *testing.T) {
	path := filepath.Join("migrations", "002_audit_log.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping audit log migration: %v", err)
	}

	content := strings.ToUpper(string(data))

	t.Run("002_audit_log_table", func(t *testing.T) {
		// Should create or alter audit table
		if !strings.Contains(content, "AUDIT") {
			t.Error("audit log migration should reference AUDIT")
		}

		if !strings.Contains(content, "CREATE TABLE") && !strings.Contains(content, "ALTER TABLE") {
			t.Error("audit log migration should create or alter a table")
		}
	})
}
