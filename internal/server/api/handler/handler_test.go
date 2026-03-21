package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/api/handler"
)

// --- In-memory repositories for handler tests ---

type memAgentRepo struct {
	agents map[string]*domain.Agent
}

func newMemAgentRepo() *memAgentRepo {
	return &memAgentRepo{agents: make(map[string]*domain.Agent)}
}

func (r *memAgentRepo) Create(_ context.Context, a *domain.Agent) error {
	if _, exists := r.agents[a.ID]; exists {
		return domain.ErrAlreadyExists
	}
	r.agents[a.ID] = a
	return nil
}

func (r *memAgentRepo) Get(_ context.Context, id string) (*domain.Agent, error) {
	a, ok := r.agents[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return a, nil
}

func (r *memAgentRepo) List(_ context.Context) ([]*domain.Agent, error) {
	var agents []*domain.Agent
	for _, a := range r.agents {
		agents = append(agents, a)
	}
	return agents, nil
}

func (r *memAgentRepo) UpdateHeartbeat(_ context.Context, id string, hb domain.Heartbeat) error {
	a, ok := r.agents[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.LastHeartbeat = hb.Timestamp
	a.Status = hb.Status
	a.Version = hb.Version
	return nil
}

func (r *memAgentRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.agents[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.agents, id)
	return nil
}

type memPOPRepo struct {
	pops map[string]*domain.POP
}

func newMemPOPRepo() *memPOPRepo {
	return &memPOPRepo{pops: make(map[string]*domain.POP)}
}

func (r *memPOPRepo) Create(_ context.Context, p *domain.POP) error {
	if _, exists := r.pops[p.Name]; exists {
		return domain.ErrAlreadyExists
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	r.pops[p.Name] = p
	return nil
}

func (r *memPOPRepo) Get(_ context.Context, name string) (*domain.POP, error) {
	p, ok := r.pops[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *memPOPRepo) List(_ context.Context) ([]*domain.POP, error) {
	var pops []*domain.POP
	for _, p := range r.pops {
		pops = append(pops, p)
	}
	return pops, nil
}

func (r *memPOPRepo) Delete(_ context.Context, name string) error {
	if _, ok := r.pops[name]; !ok {
		return domain.ErrNotFound
	}
	delete(r.pops, name)
	return nil
}

type memTestRepo struct {
	tests map[string]*domain.TestDefinition
}

func newMemTestRepo() *memTestRepo {
	return &memTestRepo{tests: make(map[string]*domain.TestDefinition)}
}

func (r *memTestRepo) Create(_ context.Context, t *domain.TestDefinition) error {
	if _, exists := r.tests[t.ID]; exists {
		return domain.ErrAlreadyExists
	}
	r.tests[t.ID] = t
	return nil
}

func (r *memTestRepo) Get(_ context.Context, id string) (*domain.TestDefinition, error) {
	t, ok := r.tests[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return t, nil
}

func (r *memTestRepo) List(_ context.Context) ([]*domain.TestDefinition, error) {
	var tests []*domain.TestDefinition
	for _, t := range r.tests {
		tests = append(tests, t)
	}
	return tests, nil
}

func (r *memTestRepo) Update(_ context.Context, t *domain.TestDefinition) error {
	if _, ok := r.tests[t.ID]; !ok {
		return domain.ErrNotFound
	}
	r.tests[t.ID] = t
	return nil
}

func (r *memTestRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.tests[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.tests, id)
	return nil
}

func (r *memTestRepo) ListByPOP(_ context.Context, _ string) ([]*domain.TestDefinition, error) {
	var tests []*domain.TestDefinition
	for _, t := range r.tests {
		if t.Enabled {
			tests = append(tests, t)
		}
	}
	return tests, nil
}

type memAssignmentRepo struct {
	assignments []*domain.TestAssignment
}

func newMemAssignmentRepo() *memAssignmentRepo {
	return &memAssignmentRepo{}
}

func (r *memAssignmentRepo) Assign(_ context.Context, testID, popName string) error {
	r.assignments = append(r.assignments, &domain.TestAssignment{
		TestID:  testID,
		POPName: popName,
	})
	return nil
}

func (r *memAssignmentRepo) Unassign(_ context.Context, _, _ string) error {
	return nil
}

func (r *memAssignmentRepo) ListByTest(_ context.Context, testID string) ([]*domain.TestAssignment, error) {
	var result []*domain.TestAssignment
	for _, a := range r.assignments {
		if a.TestID == testID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *memAssignmentRepo) ListByPOP(_ context.Context, popName string) ([]*domain.TestAssignment, error) {
	var result []*domain.TestAssignment
	for _, a := range r.assignments {
		if a.POPName == popName || a.POPName == "" {
			result = append(result, a)
		}
	}
	return result, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Agent handler tests ---

func TestAgentRegister(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	_ = pops.Create(context.Background(), &domain.POP{Name: "us-east-1-aws"})
	h := handler.NewAgentHandler(agents, pops, testLogger())

	body := `{"id":"agent-1","pop_name":"us-east-1-aws","version":"0.1.0","capabilities":["ping","dns"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var agent domain.Agent
	if err := json.Unmarshal(rr.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.ID != "agent-1" || agent.POPName != "us-east-1-aws" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
}

func TestAgentRegisterDuplicate(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	_ = pops.Create(context.Background(), &domain.POP{Name: "us-east-1-aws"})
	h := handler.NewAgentHandler(agents, pops, testLogger())

	body := `{"id":"agent-1","pop_name":"us-east-1-aws","version":"0.1.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 first time, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr = httptest.NewRecorder()
	h.Register(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate, got %d", rr.Code)
	}
}

func TestAgentRegisterMissingPOP(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	h := handler.NewAgentHandler(agents, pops, testLogger())

	body := `{"id":"agent-1","pop_name":"nonexistent","version":"0.1.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing pop, got %d", rr.Code)
	}
}

func TestAgentGet(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	_ = pops.Create(context.Background(), &domain.POP{Name: "us-east-1-aws"})
	h := handler.NewAgentHandler(agents, pops, testLogger())

	// Register first.
	body := `{"id":"agent-1","pop_name":"us-east-1-aws","version":"0.1.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	// Get via chi context.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "agent-1")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAgentGetNotFound(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	h := handler.NewAgentHandler(agents, pops, testLogger())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/nonexistent", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAgentHeartbeat(t *testing.T) {
	agents := newMemAgentRepo()
	pops := newMemPOPRepo()
	_ = pops.Create(context.Background(), &domain.POP{Name: "us-east-1-aws"})
	h := handler.NewAgentHandler(agents, pops, testLogger())

	// Register.
	body := `{"id":"agent-1","pop_name":"us-east-1-aws","version":"0.1.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	// Heartbeat.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "agent-1")
	hbBody := `{"version":"0.1.1","status":"online","active_tests":3}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/heartbeat", bytes.NewBufferString(hbBody))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Heartbeat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Test definition handler tests ---

func TestTestCRUD(t *testing.T) {
	tests := newMemTestRepo()
	assignments := newMemAssignmentRepo()
	h := handler.NewTestHandler(tests, assignments, testLogger())

	// Create.
	body := `{"id":"test-1","name":"Ping Google","test_type":"ping","target":"8.8.8.8","interval_ms":60000,"timeout_ms":5000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Create(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Get.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-1")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tests/test-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Get(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rr.Code)
	}

	// Update.
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-1")
	updateBody := `{"name":"Ping Google DNS","target":"8.8.4.4"}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/tests/test-1", bytes.NewBufferString(updateBody))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Update(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Delete.
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-1")
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/tests/test-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Delete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rr.Code)
	}
}

func TestTestCreateValidation(t *testing.T) {
	tests := newMemTestRepo()
	assignments := newMemAssignmentRepo()
	h := handler.NewTestHandler(tests, assignments, testLogger())

	cases := []struct {
		name string
		body string
		code int
	}{
		{"missing id", `{"name":"x","test_type":"ping","target":"1.1.1.1","interval_ms":1000,"timeout_ms":1000}`, 400},
		{"missing name", `{"id":"t","test_type":"ping","target":"1.1.1.1","interval_ms":1000,"timeout_ms":1000}`, 400},
		{"invalid type", `{"id":"t","name":"x","test_type":"invalid","target":"1.1.1.1","interval_ms":1000,"timeout_ms":1000}`, 400},
		{"missing target", `{"id":"t","name":"x","test_type":"ping","interval_ms":1000,"timeout_ms":1000}`, 400},
		{"zero interval", `{"id":"t","name":"x","test_type":"ping","target":"1.1.1.1","interval_ms":0,"timeout_ms":1000}`, 400},
		{"negative timeout", `{"id":"t","name":"x","test_type":"ping","target":"1.1.1.1","interval_ms":1000,"timeout_ms":-1}`, 400},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(tc.body))
			rr := httptest.NewRecorder()
			h.Create(rr, req)
			if rr.Code != tc.code {
				t.Fatalf("expected %d, got %d: %s", tc.code, rr.Code, rr.Body.String())
			}
		})
	}
}

// --- POP handler tests ---

func TestPOPCRUD(t *testing.T) {
	pops := newMemPOPRepo()
	h := handler.NewPOPHandler(pops, testLogger())

	// Create.
	body := `{"name":"us-east-1-aws","provider":"aws","city":"Ashburn","country":"US"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pops", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	h.Create(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// List.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/pops", nil)
	rr = httptest.NewRecorder()
	h.List(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rr.Code)
	}

	// Get.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "us-east-1-aws")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/pops/us-east-1-aws", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Get(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rr.Code)
	}

	// Delete.
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("name", "us-east-1-aws")
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/pops/us-east-1-aws", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	h.Delete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rr.Code)
	}
}

// --- Config sync tests ---

func TestConfigSync(t *testing.T) {
	tests := newMemTestRepo()
	_ = tests.Create(context.Background(), &domain.TestDefinition{
		ID: "t1", Name: "Ping", TestType: "ping", Target: "8.8.8.8",
		IntervalMS: 60000, TimeoutMS: 5000, Enabled: true, Config: json.RawMessage("{}"),
	})

	h := handler.NewSyncHandler(tests, testLogger())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "agent-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-1/config?pop=us-east-1-aws", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ConfigSync(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	testList, ok := resp["tests"].([]any)
	if !ok || len(testList) != 1 {
		t.Fatalf("expected 1 test, got %v", resp["tests"])
	}
}

func TestConfigSyncMissingPOP(t *testing.T) {
	tests := newMemTestRepo()
	h := handler.NewSyncHandler(tests, testLogger())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "agent-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-1/config", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ConfigSync(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// --- Healthz test ---

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.Healthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
