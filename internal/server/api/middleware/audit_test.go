package middleware_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/api/middleware"
)

// --- In-memory audit repository for middleware tests ---
// Must be goroutine-safe because AuditLogger fires Record() in a background goroutine.

type memAuditRepoForMiddleware struct {
	mu      sync.Mutex
	entries []*domain.AuditEntry
}

func newMemAuditRepoForMiddleware() *memAuditRepoForMiddleware {
	return &memAuditRepoForMiddleware{entries: []*domain.AuditEntry{}}
}

func (r *memAuditRepoForMiddleware) Record(_ context.Context, entry *domain.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.ID = int64(len(r.entries) + 1)
	r.entries = append(r.entries, entry)
	return nil
}

func (r *memAuditRepoForMiddleware) List(_ context.Context, limit, offset int) ([]*domain.AuditEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries, nil
}

func (r *memAuditRepoForMiddleware) ListByResource(_ context.Context, resource, resourceID string, limit int) ([]*domain.AuditEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var filtered []*domain.AuditEntry
	for _, e := range r.entries {
		if e.Resource == resource && e.ResourceID == resourceID {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// getEntries returns a snapshot of recorded entries (goroutine-safe).
func (r *memAuditRepoForMiddleware) getEntries() []*domain.AuditEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]*domain.AuditEntry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// TestAuditLogger_IsMutation_POST verifies that POST is recognized as a mutation.
func TestAuditLogger_IsMutation_POST(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"test"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Wait for async audit goroutine.
	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) == 0 {
		t.Error("POST request should be audited as a mutation")
	}
}

// TestAuditLogger_IsMutation_PUT verifies that PUT is recognized as a mutation.
func TestAuditLogger_IsMutation_PUT(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/123", strings.NewReader(`{"name":"updated"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) == 0 {
		t.Error("PUT request should be audited as a mutation")
	}
}

// TestAuditLogger_IsMutation_PATCH verifies that PATCH is recognized as a mutation.
func TestAuditLogger_IsMutation_PATCH(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/123", strings.NewReader(`{"enabled":false}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) == 0 {
		t.Error("PATCH request should be audited as a mutation")
	}
}

// TestAuditLogger_IsMutation_DELETE verifies that DELETE is recognized as a mutation.
func TestAuditLogger_IsMutation_DELETE(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) == 0 {
		t.Error("DELETE request should be audited as a mutation")
	}
}

// TestAuditLogger_NotMutation_GET verifies that GET is not audited.
func TestAuditLogger_NotMutation_GET(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) > 0 {
		t.Error("GET request should not be audited")
	}
}

// TestAuditLogger_NotMutation_HEAD verifies that HEAD is not audited.
func TestAuditLogger_NotMutation_HEAD(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodHead, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) > 0 {
		t.Error("HEAD request should not be audited")
	}
}

// TestAuditLogger_NotMutation_OPTIONS verifies that OPTIONS is not audited.
func TestAuditLogger_NotMutation_OPTIONS(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) > 0 {
		t.Error("OPTIONS request should not be audited")
	}
}

// TestAuditLogger_OnlySuccessful verifies that only successful mutations are audited.
func TestAuditLogger_OnlySuccessful(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	// Test with 400 error
	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.getEntries()) > 0 {
		t.Error("failed mutation (status 400) should not be audited")
	}
}

// TestAuditLogger_RecordsSuccessful verifies that successful mutations are recorded.
func TestAuditLogger_RecordsSuccessful(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"test"}`))
	ctx := context.WithValue(req.Context(), middleware.AuthIDKey, "user-123")
	ctx = context.WithValue(ctx, middleware.AuthRoleKey, "admin")
	req = req.WithContext(ctx)
	req.Header.Set("X-Forwarded-For", "192.168.1.1")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	entries := repo.getEntries()
	if len(entries) == 0 {
		t.Fatal("successful mutation should be recorded")
	}

	entry := entries[0]
	if entry.ActorID != "user-123" {
		t.Errorf("expected actor_id user-123, got %s", entry.ActorID)
	}
	if entry.ActorRole != "admin" {
		t.Errorf("expected actor_role admin, got %s", entry.ActorRole)
	}
	if entry.SourceIP != "192.168.1.1" {
		t.Errorf("expected source_ip 192.168.1.1, got %s", entry.SourceIP)
	}
}

// TestAuditLogger_ExtractIP_XForwardedFor verifies X-Forwarded-For extraction.
func TestAuditLogger_ExtractIP_XForwardedFor(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{}`))
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	entries := repo.getEntries()
	if len(entries) > 0 {
		if entries[0].SourceIP != "10.0.0.1" {
			t.Errorf("should extract first IP from X-Forwarded-For, got %s", entries[0].SourceIP)
		}
	}
}

// TestAuditLogger_ExtractIP_XRealIP verifies X-Real-IP extraction.
func TestAuditLogger_ExtractIP_XRealIP(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{}`))
	req.Header.Set("X-Real-IP", "10.0.0.3")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	entries := repo.getEntries()
	if len(entries) > 0 {
		if entries[0].SourceIP != "10.0.0.3" {
			t.Errorf("should extract IP from X-Real-IP, got %s", entries[0].SourceIP)
		}
	}
}

// TestAuditLogger_ExtractIP_RemoteAddr verifies RemoteAddr fallback.
func TestAuditLogger_ExtractIP_RemoteAddr(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{}`))
	req.RemoteAddr = "10.0.0.4:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	entries := repo.getEntries()
	if len(entries) > 0 {
		// Should strip port
		if entries[0].SourceIP != "10.0.0.4" {
			t.Errorf("should extract and strip port from RemoteAddr, got %s", entries[0].SourceIP)
		}
	}
}

// TestAuditLogger_MethodToAction verifies HTTP method mapping to audit actions.
func TestAuditLogger_MethodToAction(t *testing.T) {
	tests := []struct {
		method         string
		expectedAction string
	}{
		{http.MethodPost, "create"},
		{http.MethodPut, "update"},
		{http.MethodPatch, "update"},
		{http.MethodDelete, "delete"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			repo := newMemAuditRepoForMiddleware()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auditMiddleware := middleware.AuditLogger(repo, logger)

			handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			var body string
			if tt.method == http.MethodDelete {
				body = ""
			} else {
				body = `{}`
			}

			req := httptest.NewRequest(tt.method, "/api/v1/agents", strings.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			time.Sleep(50 * time.Millisecond)

			entries := repo.getEntries()
			if len(entries) > 0 {
				if entries[0].Action != tt.expectedAction {
					t.Errorf("%s: expected action %s, got %s", tt.method, tt.expectedAction, entries[0].Action)
				}
			}
		})
	}
}

// TestAuditLogger_ChangeDiff verifies that request body is captured as change diff.
func TestAuditLogger_ChangeDiff(t *testing.T) {
	repo := newMemAuditRepoForMiddleware()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditMiddleware := middleware.AuditLogger(repo, logger)

	handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	payload := `{"name":"test-agent","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	entries := repo.getEntries()
	if len(entries) > 0 {
		entry := entries[0]
		if len(entry.ChangeDiff) == 0 {
			t.Error("change diff should be populated from request body")
		}

		// Verify it's valid JSON
		var diff map[string]interface{}
		if err := json.Unmarshal(entry.ChangeDiff, &diff); err != nil {
			t.Errorf("change diff should be valid JSON: %v", err)
		}
	}
}

// TestAuditLogger_TableDriven runs multiple mutation test cases in a table-driven style.
func TestAuditLogger_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		statusCode        int
		shouldAudit       bool
		expectedAction    string
	}{
		{
			name:           "POST_success",
			method:         http.MethodPost,
			statusCode:     http.StatusCreated,
			shouldAudit:    true,
			expectedAction: "create",
		},
		{
			name:           "POST_error",
			method:         http.MethodPost,
			statusCode:     http.StatusBadRequest,
			shouldAudit:    false,
			expectedAction: "",
		},
		{
			name:           "PUT_success",
			method:         http.MethodPut,
			statusCode:     http.StatusOK,
			shouldAudit:    true,
			expectedAction: "update",
		},
		{
			name:           "DELETE_success",
			method:         http.MethodDelete,
			statusCode:     http.StatusNoContent,
			shouldAudit:    true,
			expectedAction: "delete",
		},
		{
			name:           "GET_never_audited",
			method:         http.MethodGet,
			statusCode:     http.StatusOK,
			shouldAudit:    false,
			expectedAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemAuditRepoForMiddleware()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auditMiddleware := middleware.AuditLogger(repo, logger)

			handler := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))

			var body string
			if tt.method == http.MethodDelete || tt.method == http.MethodGet {
				body = ""
			} else {
				body = `{}`
			}

			req := httptest.NewRequest(tt.method, "/api/v1/agents", strings.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Wait a bit for async logging
			time.Sleep(10 * time.Millisecond)

			audited := len(repo.entries) > 0
			if audited != tt.shouldAudit {
				t.Errorf("expected audit=%v, got audit=%v", tt.shouldAudit, audited)
			}

			if audited && tt.expectedAction != "" {
				entry := repo.entries[0]
				if entry.Action != tt.expectedAction {
					t.Errorf("expected action %s, got %s", tt.expectedAction, entry.Action)
				}
			}
		})
	}
}
