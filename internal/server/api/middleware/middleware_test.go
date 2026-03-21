package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/api/middleware"
)

// --- In-memory API key repo for auth tests ---

type memAPIKeyRepo struct {
	keys map[string]*domain.APIKey
}

func newMemAPIKeyRepo() *memAPIKeyRepo {
	return &memAPIKeyRepo{keys: make(map[string]*domain.APIKey)}
}

func (r *memAPIKeyRepo) Create(_ context.Context, key *domain.APIKey) error {
	r.keys[key.KeyHash] = key
	return nil
}

func (r *memAPIKeyRepo) GetByHash(_ context.Context, hash string) (*domain.APIKey, error) {
	k, ok := r.keys[hash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return k, nil
}

func (r *memAPIKeyRepo) TouchLastUsed(_ context.Context, _ string) error {
	return nil
}

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	repo := newMemAPIKeyRepo()
	rawKey := "test-api-key-12345"
	hash := middleware.HashKey(rawKey)
	_ = repo.Create(context.Background(), &domain.APIKey{
		ID:      "key-1",
		Name:    "test",
		KeyHash: hash,
		Role:    "admin",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value(middleware.AuthRoleKey)
		if role != "admin" {
			t.Fatalf("expected role admin, got %v", role)
		}
		w.WriteHeader(http.StatusOK)
	})

	mw := middleware.APIKeyAuth(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	mw(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAPIKeyAuth_MissingToken(t *testing.T) {
	repo := newMemAPIKeyRepo()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := middleware.APIKeyAuth(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	mw(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyAuth_InvalidToken(t *testing.T) {
	repo := newMemAPIKeyRepo()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := middleware.APIKeyAuth(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad-key")
	rr := httptest.NewRecorder()
	mw(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyAuth_ExpiredKey(t *testing.T) {
	repo := newMemAPIKeyRepo()
	rawKey := "expired-key-12345"
	hash := middleware.HashKey(rawKey)
	expired := time.Now().Add(-1 * time.Hour)
	_ = repo.Create(context.Background(), &domain.APIKey{
		ID:        "key-2",
		Name:      "expired",
		KeyHash:   hash,
		Role:      "admin",
		ExpiresAt: &expired,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := middleware.APIKeyAuth(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	mw(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired key, got %d", rr.Code)
	}
}

func TestAPIKeyAuth_RoleRestriction(t *testing.T) {
	repo := newMemAPIKeyRepo()
	rawKey := "agent-key-12345"
	hash := middleware.HashKey(rawKey)
	_ = repo.Create(context.Background(), &domain.APIKey{
		ID:      "key-3",
		Name:    "agent",
		KeyHash: hash,
		Role:    "agent",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Require admin role — agent should be forbidden.
	mw := middleware.APIKeyAuth(repo, nil, "admin")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	mw(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong role, got %d", rr.Code)
	}
}

// --- Rate limiter tests ---

func TestRateLimiter_AllowsBurst(t *testing.T) {
	rl := middleware.NewRateLimiter(10, time.Second, 5)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := rl.Middleware(handler)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	rl := middleware.NewRateLimiter(1, time.Minute, 3)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := rl.Middleware(handler)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "5.6.7.8:1234"
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 4th request should be limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}
