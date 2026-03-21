package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/netvantage/netvantage/internal/server/repository"
)

// AuditHandler provides HTTP handlers for audit log access.
type AuditHandler struct {
	audit  repository.AuditRepository
	logger *slog.Logger
}

// NewAuditHandler creates a new AuditHandler.
func NewAuditHandler(audit repository.AuditRepository, logger *slog.Logger) *AuditHandler {
	return &AuditHandler{audit: audit, logger: logger}
}

// List handles GET /api/v1/audit — list audit log entries.
// Query params: limit (default 50), offset (default 0), resource, resource_id.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	resource := r.URL.Query().Get("resource")
	resourceID := r.URL.Query().Get("resource_id")

	if resource != "" && resourceID != "" {
		entries, err := h.audit.ListByResource(r.Context(), resource, resourceID, limit)
		if err != nil {
			h.logger.Error("list audit by resource failed", "error", err)
			errorResponse(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entries)
		return
	}

	entries, err := h.audit.List(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("list audit failed", "error", err)
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
