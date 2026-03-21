package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// TestHandler provides HTTP handlers for test definition management.
type TestHandler struct {
	tests       repository.TestRepository
	assignments repository.TestAssignmentRepository
	logger      *slog.Logger
}

// NewTestHandler creates a new TestHandler.
func NewTestHandler(tests repository.TestRepository, assignments repository.TestAssignmentRepository, logger *slog.Logger) *TestHandler {
	return &TestHandler{tests: tests, assignments: assignments, logger: logger}
}

// CreateTestRequest is the JSON body for test creation.
type CreateTestRequest struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	TestType   string          `json:"test_type"`
	Target     string          `json:"target"`
	IntervalMS int64           `json:"interval_ms"`
	TimeoutMS  int64           `json:"timeout_ms"`
	Config     json.RawMessage `json:"config"`
	Enabled    *bool           `json:"enabled,omitempty"`
	POPs       []string        `json:"pops,omitempty"` // POPs to assign; empty = global
}

var validTestTypes = map[string]bool{
	"ping":       true,
	"dns":        true,
	"http":       true,
	"traceroute": true,
}

// Create handles POST /api/v1/tests — create a new test definition.
func (h *TestHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTestRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}

	if err := h.validateCreateRequest(req); err != nil {
		errorResponse(w, err)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Config == nil {
		req.Config = json.RawMessage("{}")
	}

	test := &domain.TestDefinition{
		ID:         req.ID,
		Name:       req.Name,
		TestType:   req.TestType,
		Target:     req.Target,
		IntervalMS: req.IntervalMS,
		TimeoutMS:  req.TimeoutMS,
		Config:     req.Config,
		Enabled:    enabled,
	}

	if err := h.tests.Create(r.Context(), test); err != nil {
		h.logger.Error("create test failed", "test_id", req.ID, "error", err)
		errorResponse(w, err)
		return
	}

	// Create assignments.
	if len(req.POPs) == 0 {
		// Global assignment.
		_ = h.assignments.Assign(r.Context(), req.ID, "")
	} else {
		for _, pop := range req.POPs {
			_ = h.assignments.Assign(r.Context(), req.ID, pop)
		}
	}

	h.logger.Info("test created", "test_id", req.ID, "type", req.TestType, "target", req.Target)
	writeJSON(w, http.StatusCreated, test)
}

func (h *TestHandler) validateCreateRequest(req CreateTestRequest) error {
	if req.ID == "" {
		return fmt.Errorf("%w: id is required", domain.ErrInvalidInput)
	}
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", domain.ErrInvalidInput)
	}
	if !validTestTypes[req.TestType] {
		return fmt.Errorf("%w: invalid test_type %q", domain.ErrInvalidInput, req.TestType)
	}
	if req.Target == "" {
		return fmt.Errorf("%w: target is required", domain.ErrInvalidInput)
	}
	if req.IntervalMS <= 0 {
		return fmt.Errorf("%w: interval_ms must be positive", domain.ErrInvalidInput)
	}
	if req.TimeoutMS <= 0 {
		return fmt.Errorf("%w: timeout_ms must be positive", domain.ErrInvalidInput)
	}
	return nil
}

// List handles GET /api/v1/tests — list all test definitions.
func (h *TestHandler) List(w http.ResponseWriter, r *http.Request) {
	tests, err := h.tests.List(r.Context())
	if err != nil {
		h.logger.Error("list tests failed", "error", err)
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tests)
}

// Get handles GET /api/v1/tests/{id} — get a single test definition.
func (h *TestHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	test, err := h.tests.Get(r.Context(), id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, test)
}

// UpdateTestRequest is the JSON body for test updates.
type UpdateTestRequest struct {
	Name       string          `json:"name,omitempty"`
	TestType   string          `json:"test_type,omitempty"`
	Target     string          `json:"target,omitempty"`
	IntervalMS *int64          `json:"interval_ms,omitempty"`
	TimeoutMS  *int64          `json:"timeout_ms,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
	Enabled    *bool           `json:"enabled,omitempty"`
}

// Update handles PUT /api/v1/tests/{id} — update a test definition.
func (h *TestHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.tests.Get(r.Context(), id)
	if err != nil {
		errorResponse(w, err)
		return
	}

	var req UpdateTestRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}

	// Merge fields.
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.TestType != "" {
		if !validTestTypes[req.TestType] {
			errorResponse(w, fmt.Errorf("%w: invalid test_type %q", domain.ErrInvalidInput, req.TestType))
			return
		}
		existing.TestType = req.TestType
	}
	if req.Target != "" {
		existing.Target = req.Target
	}
	if req.IntervalMS != nil {
		existing.IntervalMS = *req.IntervalMS
	}
	if req.TimeoutMS != nil {
		existing.TimeoutMS = *req.TimeoutMS
	}
	if req.Config != nil {
		existing.Config = req.Config
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.tests.Update(r.Context(), existing); err != nil {
		h.logger.Error("update test failed", "test_id", id, "error", err)
		errorResponse(w, err)
		return
	}

	h.logger.Info("test updated", "test_id", id)
	writeJSON(w, http.StatusOK, existing)
}

// Delete handles DELETE /api/v1/tests/{id} — remove a test definition.
func (h *TestHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.tests.Delete(r.Context(), id); err != nil {
		errorResponse(w, err)
		return
	}
	h.logger.Info("test deleted", "test_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// AssignRequest is the JSON body for test assignment.
type AssignRequest struct {
	POPs []string `json:"pops"`
}

// Assign handles POST /api/v1/tests/{id}/assign — assign test to POPs.
func (h *TestHandler) Assign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify test exists.
	if _, err := h.tests.Get(r.Context(), id); err != nil {
		errorResponse(w, err)
		return
	}

	var req AssignRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}

	if len(req.POPs) == 0 {
		_ = h.assignments.Assign(r.Context(), id, "")
	} else {
		for _, pop := range req.POPs {
			_ = h.assignments.Assign(r.Context(), id, pop)
		}
	}

	h.logger.Info("test assigned", "test_id", id, "pops", req.POPs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}
