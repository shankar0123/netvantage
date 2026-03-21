// Package middleware provides HTTP middleware for the control plane API.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/netvantage/netvantage/internal/server/repository"
)

type contextKey string

const (
	// AuthRoleKey is the context key for the authenticated role.
	AuthRoleKey contextKey = "auth_role"
	// AuthIDKey is the context key for the authenticated API key ID.
	AuthIDKey contextKey = "auth_id"
)

// APIKeyAuth creates middleware that validates API keys via Bearer token.
// Pass allowedRoles to restrict access to specific roles (empty = any authenticated).
func APIKeyAuth(keys repository.APIKeyRepository, logger *slog.Logger, allowedRoles ...string) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	roleSet := make(map[string]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		roleSet[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				unauthorized(w)
				return
			}

			hash := hashKey(token)
			key, err := keys.GetByHash(r.Context(), hash)
			if err != nil {
				logger.Debug("auth failed: key not found")
				unauthorized(w)
				return
			}

			// Check expiry.
			if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
				logger.Debug("auth failed: key expired", "key_id", key.ID)
				unauthorized(w)
				return
			}

			// Check role if restricted.
			if len(roleSet) > 0 && !roleSet[key.Role] {
				logger.Debug("auth failed: insufficient role", "key_id", key.ID, "role", key.Role)
				forbidden(w)
				return
			}

			// Touch last_used asynchronously.
			go func() {
				_ = keys.TouchLastUsed(context.Background(), key.ID)
			}()

			ctx := context.WithValue(r.Context(), AuthRoleKey, key.Role)
			ctx = context.WithValue(ctx, AuthIDKey, key.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HashKey returns the SHA-256 hex hash of a raw API key.
// Exported for use when creating keys.
func HashKey(raw string) string {
	return hashKey(raw)
}

func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func forbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden"}`))
}
