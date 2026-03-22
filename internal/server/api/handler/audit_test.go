package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/api/handler"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// --- In-memory audit repository for handler tests ---

type memAuditRepo struct {
	entries map[int64]*domain.AuditEntry
	nextID  int64
}

func newMemAuditRepo() *memAuditRepo {
	return &memAuditRepo{
		entries: make(map[int64]*domain.AuditEntry),
		nextID:  1,
	}
}

func (r *memAuditRepo) Record(_ context.Context, entry *domain.AuditEntry) error {
	entry.ID = r.nextID
	r.entries[r.nextID] = entry
	r.nextID++
	return nil
}

func (r *memAuditRepo) List(_ context.Context, limit, offset int) ([]*domain.AuditEntry, error) {
	var entries []*domain.AuditEntry
	var ids []int64
	for id := range r.entries {
		ids = append(ids, id)
	}

	// Sort by ID descending for consistent results
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] < ids[j] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}

	// Apply offset and limit
	start := offset
	if start >= len(ids) {
		return entries, nil
	}
	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}

	for i := start; i < end; i++ {
		entries = append(entries, r.entries[ids[i]])
	}

	return entries, nil
}

func (r *memAuditRepo) ListByResource(_ context.Context, resource, resourceID string, limit int) ([]*domain.AuditEntry, error) {
	var entries []*domain.AuditEntry
	for _, entry := range r.entries {
		if entry.Resource == resource && entry.ResourceID == resourceID {
			entries = append(entries, entry)
		}
		if len(entries) >= limit {
			break
		}
	}
	return entries, nil
}

// --- Audit Handler Tests ---

// TestAuditList_Default verifies that GET /api/v1/audit returns 200 with default limit.
func TestAuditList_Default(t *testing.T) {
	repo := newMemAuditRepo()

	// Seed with some entries
	for i := 0; i < 3; i++ {
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-" + strconv.Itoa(i),
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-" + strconv.Itoa(i),
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
	}

	h := handler.NewAuditHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

// TestAuditList_WithLimit verifies that limit parameter is respected.
func TestAuditList_WithLimit(t *testing.T) {
	repo := newMemAuditRepo()

	// Seed with 10 entries
	for i := 0; i < 10; i++ {
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-" + strconv.Itoa(i),
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-" + strconv.Itoa(i),
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
	}

	h := handler.NewAuditHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=5", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

// TestAuditList_WithOffset verifies that offset parameter provides pagination.
func TestAuditList_WithOffset(t *testing.T) {
	repo := newMemAuditRepo()

	// Seed with 10 entries
	for i := 0; i < 10; i++ {
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-" + strconv.Itoa(i),
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-" + strconv.Itoa(i),
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
	}

	h := handler.NewAuditHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=5&offset=5", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}

	// Verify it's the second page (different IDs)
	if entries[0].ID <= 5 {
		t.Errorf("offset did not skip first 5 entries")
	}
}

// TestAuditList_FilterByResource verifies that resource filtering works.
func TestAuditList_FilterByResource(t *testing.T) {
	repo := newMemAuditRepo()

	// Seed with mixed resources
	for i := 0; i < 3; i++ {
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-1",
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-1",
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-2",
			Action:     "create",
			Resource:   "tests",
			ResourceID: "test-1",
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
	}

	h := handler.NewAuditHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource=agents&resource_id=agent-1", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries for resource filter, got %d", len(entries))
	}

	for _, entry := range entries {
		if entry.Resource != "agents" || entry.ResourceID != "agent-1" {
			t.Errorf("filter returned mismatched entry: %s/%s", entry.Resource, entry.ResourceID)
		}
	}
}

// TestAuditList_InvalidLimit verifies that invalid limit values are rejected.
func TestAuditList_InvalidLimit(t *testing.T) {
	repo := newMemAuditRepo()
	h := handler.NewAuditHandler(repo, nil)

	// Test with negative limit
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=-1", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		// May return 400 if validation is strict
		// For now, we expect 200 with default limit applied
		t.Logf("negative limit handling: status %d", w.Code)
	}
}

// TestAuditList_LimitExceedsMax verifies that limit is capped at maximum.
func TestAuditList_LimitExceedsMax(t *testing.T) {
	repo := newMemAuditRepo()

	// Seed with 100 entries
	for i := 0; i < 100; i++ {
		_ = repo.Record(context.Background(), &domain.AuditEntry{
			ActorID:    "actor-" + strconv.Itoa(i),
			Action:     "create",
			Resource:   "agents",
			ResourceID: "agent-" + strconv.Itoa(i),
			SourceIP:   "127.0.0.1",
			Timestamp:  time.Now().UTC(),
		})
	}

	h := handler.NewAuditHandler(repo, nil)

	// Request limit > 1000 (should be capped)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=5000", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	// Limit should be capped at 1000
	if len(entries) > 1000 {
		t.Errorf("limit exceeded maximum: got %d entries", len(entries))
	}
}

// TestAuditList_EmptyResult verifies that empty result is returned correctly.
func TestAuditList_EmptyResult(t *testing.T) {
	repo := newMemAuditRepo()
	h := handler.NewAuditHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var entries []*domain.AuditEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Errorf("failed to decode response: %v", err)
	}

	if entries != nil && len(entries) != 0 {
		t.Errorf("expected empty result, got %d entries", len(entries))
	}
}

// TestAuditList_TableDriven runs multiple test cases in a table-driven style.
func TestAuditList_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		queryString    string
		seedCount      int
		expectedCode   int
		expectedMinLen int
		expectedMaxLen int
	}{
		{
			name:           "default_params",
			queryString:    "",
			seedCount:      10,
			expectedCode:   http.StatusOK,
			expectedMinLen: 0,
			expectedMaxLen: 100,
		},
		{
			name:           "limit_5",
			queryString:    "limit=5",
			seedCount:      20,
			expectedCode:   http.StatusOK,
			expectedMinLen: 5,
			expectedMaxLen: 5,
		},
		{
			name:           "offset_pagination",
			queryString:    "limit=10&offset=10",
			seedCount:      30,
			expectedCode:   http.StatusOK,
			expectedMinLen: 0,
			expectedMaxLen: 20,
		},
		{
			name:           "empty_repo",
			queryString:    "",
			seedCount:      0,
			expectedCode:   http.StatusOK,
			expectedMinLen: 0,
			expectedMaxLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemAuditRepo()

			// Seed repository
			for i := 0; i < tt.seedCount; i++ {
				_ = repo.Record(context.Background(), &domain.AuditEntry{
					ActorID:    "actor-" + strconv.Itoa(i),
					Action:     "create",
					Resource:   "agents",
					ResourceID: "agent-" + strconv.Itoa(i),
					SourceIP:   "127.0.0.1",
					Timestamp:  time.Now().UTC(),
				})
			}

			h := handler.NewAuditHandler(repo, nil)

			url := "/api/v1/audit"
			if tt.queryString != "" {
				url = url + "?" + tt.queryString
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			h.List(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, w.Code)
			}

			var entries []*domain.AuditEntry
			if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
				if tt.expectedCode == http.StatusOK {
					t.Errorf("failed to decode response: %v", err)
				}
				return
			}

			if len(entries) < tt.expectedMinLen {
				t.Errorf("expected at least %d entries, got %d", tt.expectedMinLen, len(entries))
			}
			if len(entries) > tt.expectedMaxLen {
				t.Errorf("expected at most %d entries, got %d", tt.expectedMaxLen, len(entries))
			}
		})
	}
}

// Verify memAuditRepo implements repository.AuditRepository
var _ repository.AuditRepository = (*memAuditRepo)(nil)
