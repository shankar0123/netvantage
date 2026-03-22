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

// TestPostgres_AgentCRUD exercises the full agent lifecycle against a real
// PostgreSQL instance: Create → Get → List → UpdateHeartbeat → Delete.
// This validates SQL queries, constraint handling, and trigger behavior
// that cannot be tested with in-memory mocks.
func TestPostgres_AgentCRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, connStr := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	runMigrations(ctx, t, pool)

	agentRepo := postgres.NewAgentRepo(pool)
	popRepo := postgres.NewPOPRepo(pool)

	// Create a POP first (agents reference POPs via FK).
	pop := &domain.POP{
		Name:     "us-east-1-aws",
		Provider: "aws",
		City:     "Ashburn",
		Country:  "US",
	}
	if err := popRepo.Create(ctx, pop); err != nil {
		t.Fatalf("create POP: %v", err)
	}

	// --- Create ---
	agent := &domain.Agent{
		ID:            "agent-e2e-001",
		POPName:       "us-east-1-aws",
		Version:       "1.0.0",
		Status:        domain.AgentStatusOnline,
		Capabilities:  []string{"ping", "dns", "http"},
		Labels:        map[string]string{"env": "test"},
		LastHeartbeat: time.Now().UTC(),
		RegisteredAt:  time.Now().UTC(),
	}

	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// --- Get ---
	got, err := agentRepo.Get(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.POPName != agent.POPName {
		t.Errorf("pop_name mismatch: got %s, want %s", got.POPName, agent.POPName)
	}
	if got.Version != agent.Version {
		t.Errorf("version mismatch: got %s, want %s", got.Version, agent.Version)
	}
	if string(got.Status) != string(agent.Status) {
		t.Errorf("status mismatch: got %s, want %s", got.Status, agent.Status)
	}

	// --- List ---
	agents, err := agentRepo.List(ctx)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent in list, got %d", len(agents))
	}

	// --- UpdateHeartbeat ---
	hb := domain.Heartbeat{
		AgentID:     agent.ID,
		POPName:     agent.POPName,
		Version:     "1.0.1",
		Timestamp:   time.Now().UTC(),
		Status:      domain.AgentStatusDegraded,
		ActiveTests: 3,
	}
	if err := agentRepo.UpdateHeartbeat(ctx, agent.ID, hb); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	updated, err := agentRepo.Get(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get after heartbeat: %v", err)
	}
	if updated.Version != "1.0.1" {
		t.Errorf("version not updated by heartbeat: got %s", updated.Version)
	}

	// --- Delete ---
	if err := agentRepo.Delete(ctx, agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	_, err = agentRepo.Get(ctx, agent.ID)
	if err == nil {
		t.Error("expected error getting deleted agent, got nil")
	}
}

// TestPostgres_DuplicateAgent verifies that creating a duplicate agent ID
// returns ErrAlreadyExists rather than a raw database error.
func TestPostgres_DuplicateAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, connStr := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	runMigrations(ctx, t, pool)

	agentRepo := postgres.NewAgentRepo(pool)
	popRepo := postgres.NewPOPRepo(pool)

	pop := &domain.POP{Name: "dup-test-pop", Provider: "test"}
	if err := popRepo.Create(ctx, pop); err != nil {
		t.Fatalf("create POP: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-dup-test",
		POPName:       "dup-test-pop",
		Version:       "1.0.0",
		Status:        domain.AgentStatusOnline,
		Labels:        map[string]string{},
		LastHeartbeat: time.Now().UTC(),
		RegisteredAt:  time.Now().UTC(),
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, connStr := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	runMigrations(ctx, t, pool)

	testRepo := postgres.NewTestRepo(pool)

	td := &domain.TestDefinition{
		ID:         "test-e2e-ping-1",
		Name:       "E2E Ping Test",
		TestType:   "ping",
		Target:     "8.8.8.8",
		IntervalMS: 30000,
		TimeoutMS:  5000,
		Config:     []byte(`{"count":5,"interval_ms":200}`),
		Enabled:    true,
	}

	// Create.
	if err := testRepo.Create(ctx, td); err != nil {
		t.Fatalf("create test: %v", err)
	}

	// Get.
	got, err := testRepo.Get(ctx, td.ID)
	if err != nil {
		t.Fatalf("get test: %v", err)
	}
	if got.Name != td.Name {
		t.Errorf("name mismatch: got %s, want %s", got.Name, td.Name)
	}
	if got.IntervalMS != td.IntervalMS {
		t.Errorf("interval mismatch: got %d, want %d", got.IntervalMS, td.IntervalMS)
	}

	// Update.
	td.Enabled = false
	if err := testRepo.Update(ctx, td); err != nil {
		t.Fatalf("update test: %v", err)
	}
	updated, err := testRepo.Get(ctx, td.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if updated.Enabled {
		t.Error("expected Enabled=false after update")
	}

	// List.
	tests, err := testRepo.List(ctx)
	if err != nil {
		t.Fatalf("list tests: %v", err)
	}
	if len(tests) != 1 {
		t.Errorf("expected 1 test, got %d", len(tests))
	}

	// Delete.
	if err := testRepo.Delete(ctx, td.ID); err != nil {
		t.Fatalf("delete test: %v", err)
	}
}

// TestPostgres_AuditLog verifies audit log recording and querying with real
// PostgreSQL, including pagination (offset/limit) which was previously buggy
// with in-memory repos.
func TestPostgres_AuditLog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, connStr := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	runMigrations(ctx, t, pool)

	auditRepo := postgres.NewAuditRepo(pool)

	// Seed 20 entries.
	for i := 0; i < 20; i++ {
		entry := &domain.AuditEntry{
			Timestamp:  time.Now().UTC(),
			ActorID:    "admin-e2e",
			ActorRole:  "admin",
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-" + time.Now().Format("150405.000"),
			SourceIP:   "127.0.0.1",
		}
		if err := auditRepo.Record(ctx, entry); err != nil {
			t.Fatalf("record audit entry %d: %v", i, err)
		}
	}

	// List with default (should return entries).
	entries, err := auditRepo.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) != 20 {
		t.Errorf("expected 20 entries, got %d", len(entries))
	}

	// List with limit.
	limited, err := auditRepo.List(ctx, 5, 0)
	if err != nil {
		t.Fatalf("list with limit: %v", err)
	}
	if len(limited) != 5 {
		t.Errorf("expected 5 entries with limit, got %d", len(limited))
	}

	// List with offset — this tests real SQL pagination that the in-memory
	// repo previously got wrong.
	page2, err := auditRepo.List(ctx, 5, 5)
	if err != nil {
		t.Fatalf("list with offset: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("expected 5 entries on page 2, got %d", len(page2))
	}

	// Verify pages don't overlap.
	if len(limited) > 0 && len(page2) > 0 && limited[0].ID == page2[0].ID {
		t.Error("page 1 and page 2 returned same first entry — offset not working")
	}

	// ListByResource.
	byResource, err := auditRepo.ListByResource(ctx, "agents", entries[0].ResourceID, 10)
	if err != nil {
		t.Fatalf("list by resource: %v", err)
	}
	if len(byResource) == 0 {
		t.Error("expected at least 1 entry for resource filter")
	}
}

// TestPostgres_MigrationsIdempotent verifies that running migrations twice
// does not cause errors (all statements use IF NOT EXISTS / ON CONFLICT).
func TestPostgres_MigrationsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, connStr := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Run migrations twice — second run must not error.
	runMigrations(ctx, t, pool)
	runMigrations(ctx, t, pool)
}
