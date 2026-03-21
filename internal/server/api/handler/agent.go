package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// AgentHandler provides HTTP handlers for agent management.
type AgentHandler struct {
	agents repository.AgentRepository
	pops   repository.POPRepository
	logger *slog.Logger
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(agents repository.AgentRepository, pops repository.POPRepository, logger *slog.Logger) *AgentHandler {
	return &AgentHandler{agents: agents, pops: pops, logger: logger}
}

// RegisterRequest is the JSON body for agent registration.
type RegisterRequest struct {
	ID           string            `json:"id"`
	POPName      string            `json:"pop_name"`
	Version      string            `json:"version"`
	Capabilities []string          `json:"capabilities"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// Register handles POST /api/v1/agents — register a new agent.
func (h *AgentHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}
	if req.ID == "" || req.POPName == "" {
		errorResponse(w, fmt.Errorf("%w: id and pop_name are required", domain.ErrInvalidInput))
		return
	}

	// Verify POP exists.
	if _, err := h.pops.Get(r.Context(), req.POPName); err != nil {
		errorResponse(w, fmt.Errorf("%w: pop %q not found", domain.ErrInvalidInput, req.POPName))
		return
	}

	now := time.Now().UTC()
	agent := &domain.Agent{
		ID:            req.ID,
		POPName:       req.POPName,
		Version:       req.Version,
		Status:        domain.AgentStatusOnline,
		Capabilities:  req.Capabilities,
		Labels:        req.Labels,
		LastHeartbeat: now,
		RegisteredAt:  now,
	}

	if err := h.agents.Create(r.Context(), agent); err != nil {
		h.logger.Error("agent registration failed", "agent_id", req.ID, "error", err)
		errorResponse(w, err)
		return
	}

	h.logger.Info("agent registered", "agent_id", req.ID, "pop", req.POPName)
	writeJSON(w, http.StatusCreated, agent)
}

// List handles GET /api/v1/agents — list all agents.
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	agents, err := h.agents.List(r.Context())
	if err != nil {
		h.logger.Error("list agents failed", "error", err)
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

// Get handles GET /api/v1/agents/{id} — get a single agent.
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := h.agents.Get(r.Context(), id)
	if err != nil {
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

// Delete handles DELETE /api/v1/agents/{id} — deregister an agent.
func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.agents.Delete(r.Context(), id); err != nil {
		errorResponse(w, err)
		return
	}
	h.logger.Info("agent deleted", "agent_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// HeartbeatRequest is the JSON body for agent heartbeats.
type HeartbeatRequest struct {
	Version     string             `json:"version"`
	Status      domain.AgentStatus `json:"status"`
	ActiveTests int                `json:"active_tests"`
}

// Heartbeat handles POST /api/v1/agents/{id}/heartbeat — agent liveness signal.
func (h *AgentHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req HeartbeatRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}

	hb := domain.Heartbeat{
		AgentID:     id,
		Version:     req.Version,
		Timestamp:   time.Now().UTC(),
		Status:      req.Status,
		ActiveTests: req.ActiveTests,
	}

	if err := h.agents.UpdateHeartbeat(r.Context(), id, hb); err != nil {
		errorResponse(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
