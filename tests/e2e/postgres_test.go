//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/repository/postgres"
)

// runMigrations applies all SQL migration files against the given pool.
func runMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	migrationsDir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("failed to read migrations dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", entry.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("failed to execute migration %s: %v", entry.Name(), err)
		}
		t.Logf("applied migration: %s", entry.Name())
	}
}

// resetDB drops and recreates all tables for test isolation.
func resetDB(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// Drop tables in dependency order.
	tables := []string{"audit_log", "test_assignments", "agents", "test_definitions", "api_keys", "pops"}
	for _, table := range tables {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+table+" CASCADE")
	}
	// Drop the trigger function too.
	_, _ = pool.Exec(ctx, "DROP FUNCTION IF EXISTS update_updated_at() CASCADE")
	runMigrations(ctx, t, pool)
}

// TestPostgres_AgentCRUD exercises the full agent lifecycle against real PostgreSQL.
func TestPostgres_AgentCRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, sharedPGConnStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	resetDB(ctx, t, pool)

	agentRepo := postgres.NewAgentRepo(pool)
	popRepo := postgres.NewPOPRepo(pool)

	pop := &domain.POP{Name: "us-east-1-aws", Provider: "aws", City: "Ashburn", Country: "US"}
	if err := popRepo.Create(ctx, pop); err != nil {
		t.Fatalf("create POP: %v", err)
	}

	agent := &domain.Agent{
		ID: "agent-e2e-001", POPName: "us-east-1-aws", Version: "1.0.0",
		Status: domain.AgentStatusOnline, Capabilities: []string{"ping", "dns", "http"},
		Labels: map[string]string{"env": "test"}, LastHeartbeat: time.Now().UTC(),
		RegisteredAt: time.Now().UTC(),
	}

	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	got, err := agentRepo.Get(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.POPName != agent.POPName {
		t.Errorf("pop_name mismatch: got %s, want %s", got.POPName, agent.POPName)
	}

	agents, err := agentRepo.List(ctx)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}

	hb := domain.Heartbeat{
		AgentID: agent.ID, POPName: agent.POPName, Version: "1.0.1",
		Timestamp: time.Now().UTC(), Status: domain.AgentStatusDegraded, ActiveTests: 3,
	}
	if err := agentRepo.UpdateHeartbeat(ctx, agent.ID, hb); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	if err := agentRepo.Delete(ctx, agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	_, err = agentRepo.Get(ctx, agent.ID)
	if err == nil {
		t.Error("expected error getting deleted agent, got nil")
	}
}

// TestPostgres_DuplicateAgent verifies ErrAlreadyExists on duplicate insert.
func TestPostgres_DuplicateAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, sharedPGConnStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	resetDB(ctx, t, pool)

	agentRepo := postgres.NewAgentRepo(pool)
	popRepo := postgres.NewPOPRepo(pool)

	pop := &domain.POP{Name: "dup-test-pop", Provider: "test"}
	if err := popRepo.Create(ctx, pop); err != nil {
		t.Fatalf("create POP: %v", err)
	}

	agent := &domain.Agent{
		ID: "agent-dup-test", POPName: "dup-test-pop", Version: "1.0.0",
		Status: domain.AgentStatusOnline, Labels: map[string]string{},
		LastHeartbeat: time.Now().UTC(), RegisteredAt: time.Now().UTC(),
	}

	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("first create: %v", err)
	}

	err = agentRepo.Create(ctx, agent)
	if err == nil {
		t.Error("expected error on duplicate create, got nil")
	}
	if err != domain.ErrAlreadyExists {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

// TestPostgres_TestDefinitionCRUD exercises the test definition lifecycle.
func TestPostgres_TestDefinitionCRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, sharedPGConnStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	resetDB(ctx, t, pool)

	testRepo := postgres.NewTestRepo(pool)

	td := &domain.TestDefinition{
		ID: "test-e2e-ping-1", Name: "E2E Ping Test", TestType: "ping",
		Target: "8.8.8.8", IntervalMS: 30000, TimeoutMS: 5000,
		Config: []byte(`{"count":5,"interval_ms":200}`), Enabled: true,
	}

	if err := testRepo.Create(ctx, td); err != nil {
		t.Fatalf("create test: %v", err)
	}

	got, err := testRepo.Get(ctx, td.ID)
	if err != nil {
		t.Fatalf("get test: %v", err)
	}
	if got.Name != td.Name {
		t.Errorf("name mismatch: got %s, want %s", got.Name, td.Name)
	}

	td.Enabled = false
	if err := testRepo.Update(ctx, td); err != nil {
		t.Fatalf("update test: %v", err)
	}

	if err := testRepo.Delete(ctx, td.ID); err != nil {
		t.Fatalf("delete test: %v", err)
	}
}

// TestPostgres_AuditLog verifies audit log recording and pagination.
func TestPostgres_AuditLog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, sharedPGConnStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	resetDB(ctx, t, pool)

	auditRepo := postgres.NewAuditRepo(pool)

	for i := 0; i < 20; i++ {
		entry := &domain.AuditEntry{
			Timestamp: time.Now().UTC(), ActorID: "admin-e2e", ActorRole: "admin",
			Action: "create", Resource: "agents", ResourceID: fmt.Sprintf("agent-%d", i),
			SourceIP: "127.0.0.1",
		}
		if err := auditRepo.Record(ctx, entry); err != nil {
			t.Fatalf("record audit entry %d: %v", i, err)
		}
	}

	entries, err := auditRepo.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) != 20 {
		t.Errorf("expected 20 entries, got %d", len(entries))
	}

	limited, err := auditRepo.List(ctx, 5, 0)
	if err != nil {
		t.Fatalf("list with limit: %v", err)
	}
	if len(limited) != 5 {
		t.Errorf("expected 5 entries with limit, got %d", len(limited))
	}

	page2, err := auditRepo.List(ctx, 5, 5)
	if err != nil {
		t.Fatalf("list with offset: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("expected 5 entries on page 2, got %d", len(page2))
	}

	if len(limited) > 0 && len(page2) > 0 && limited[0].ID == page2[0].ID {
		t.Error("page 1 and page 2 returned same first entry — offset not working")
	}
}

// TestPostgres_MigrationsIdempotent verifies that running migrations twice is safe.
func TestPostgres_MigrationsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, sharedPGConnStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	resetDB(ctx, t, pool)
	// Second run must not error.
	runMigrations(ctx, t, pool)
}
