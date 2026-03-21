package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/server/repository"
)

// SyncHandler provides the config sync endpoint for agents.
type SyncHandler struct {
	tests  repository.TestRepository
	logger *slog.Logger
}

// NewSyncHandler creates a new SyncHandler.
func NewSyncHandler(tests repository.TestRepository, logger *slog.Logger) *SyncHandler {
	return &SyncHandler{tests: tests, logger: logger}
}

// ConfigSync handles GET /api/v1/agents/{id}/config — returns assigned tests for the agent's POP.
// The agent includes its POP name as a query parameter.
func (h *SyncHandler) ConfigSync(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	popName := r.URL.Query().Get("pop")

	if popName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "pop query parameter is required",
		})
		return
	}

	tests, err := h.tests.ListByPOP(r.Context(), popName)
	if err != nil {
		h.logger.Error("config sync failed", "agent_id", agentID, "pop", popName, "error", err)
		errorResponse(w, err)
		return
	}

	h.logger.Debug("config sync", "agent_id", agentID, "pop", popName, "tests", len(tests))
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID,
		"pop":      popName,
		"tests":    tests,
	})
}
