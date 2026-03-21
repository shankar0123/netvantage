package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// AuditLogger creates middleware that records an audit entry for every
// state-mutating API request (POST, PUT, DELETE, PATCH).
// Read-only requests (GET, HEAD, OPTIONS) are not audited.
func AuditLogger(audit repository.AuditRepository, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit mutations.
			if !isMutation(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Capture the request body for the change diff.
			var bodyBytes []byte
			if r.Body != nil {
				bodyBytes, _ = io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			// Wrap the response writer to capture the status code.
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			// Execute the handler.
			next.ServeHTTP(sw, r)

			// Only record audit entries for successful mutations.
			if sw.status >= 200 && sw.status < 400 {
				resource, resourceID := extractResource(r)
				action := methodToAction(r.Method)

				entry := &domain.AuditEntry{
					Timestamp:  time.Now().UTC(),
					ActorID:    extractActorID(r.Context()),
					ActorRole:  extractActorRole(r.Context()),
					Action:     action,
					Resource:   resource,
					ResourceID: resourceID,
					SourceIP:   extractIP(r),
				}

				// Include request body as change diff (truncated).
				if len(bodyBytes) > 0 && len(bodyBytes) < 10240 {
					entry.ChangeDiff = bodyBytes
				}

				// Fire and forget — audit logging should never block the response.
				go func() {
					if err := audit.Record(context.Background(), entry); err != nil {
						logger.Error("audit log failed",
							"action", entry.Action,
							"resource", entry.Resource,
							"resource_id", entry.ResourceID,
							"error", err,
						)
					}
				}()
			}
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
	written bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// isMutation returns true for HTTP methods that change state.
func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// methodToAction maps an HTTP method to an audit action string.
func methodToAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return method
	}
}

// extractResource parses the chi route pattern to determine the resource type
// and specific resource ID from the URL.
func extractResource(r *http.Request) (resource, resourceID string) {
	routeCtx := chi.RouteContext(r.Context())
	if routeCtx == nil {
		return "unknown", ""
	}

	pattern := routeCtx.RoutePattern()
	// Pattern looks like "/api/v1/agents/{id}" or "/api/v1/tests/{id}/assign"
	parts := strings.Split(strings.TrimPrefix(pattern, "/api/v1/"), "/")
	if len(parts) > 0 {
		resource = parts[0]
	}

	// Try to extract the resource ID from URL params.
	if id := chi.URLParam(r, "id"); id != "" {
		resourceID = id
	} else if name := chi.URLParam(r, "name"); name != "" {
		resourceID = name
	}

	// For sub-resource actions like /tests/{id}/assign, append the action.
	if len(parts) > 2 {
		resource = resource + "." + parts[len(parts)-1]
	}

	return resource, resourceID
}

// extractActorID extracts the authenticated actor ID from the request context.
func extractActorID(ctx context.Context) string {
	if id, ok := ctx.Value(AuthIDKey).(string); ok {
		return id
	}
	return "anonymous"
}

// extractActorRole extracts the authenticated actor role from the request context.
func extractActorRole(ctx context.Context) string {
	if role, ok := ctx.Value(AuthRoleKey).(string); ok {
		return role
	}
	return "unknown"
}

// extractIP returns the client IP address, preferring X-Forwarded-For.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}
