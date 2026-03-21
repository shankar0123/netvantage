package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/domain"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// POPHandler provides HTTP handlers for POP management.
type POPHandler struct {
	pops   repository.POPRepository
	logger *slog.Logger
}

// NewPOPHandler creates a new POPHandler.
func NewPOPHandler(pops repository.POPRepository, logger *slog.Logger) *POPHandler {
	return &POPHandler{pops: pops, logger: logger}
}

// CreateRequest is the JSON body for POP creation.
type CreatePOPRequest struct {
	Name      string            `json:"name"`
	Provider  string            `json:"provider,omitempty"`
	City      string            `json:"city,omitempty"`
	Country   string            `json:"country,omitempty"`
	Latitude  *float64          `json:"latitude,omitempty"`
	Longitude *float64          `json:"longitude,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Create handles POST /api/v1/pops — create a new POP.
func (h *POPHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreatePOPRequest
	if err := readJSON(r, &req); err != nil {
		errorResponse(w, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err))
		return
	}
	if req.Name == "" {
		errorResponse(w, fmt.Errorf("%w: name is required", domain.ErrInvalidInput))
		return
	}

	pop := &domain.POP{
		Name:      req.Name,
		Provider:  req.Provider,
		City:      req.City,
		Country:   req.Country,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Labels:    req.Labels,
	}

	if err := h.pops.Create(r.Context(), pop); err != nil {
		h.logger.Error("create pop failed", "pop", req.Name, "error", err)
		errorResponse(w, err)
		return
	}

	h.logger.Info("pop created", "pop", req.Name)
	writeJSON(w, http.StatusCreated, pop)
}

// List handles GET /api/v1/pops — list all POPs.
func (h *POPHandler) List(w http.ResponseWriter, r *http.Request) {
	pops, err := h.pops.List(r.Context())
	if err != nil {
		h.logger.Error("list pops failed", "error", err)
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pops)
}

// Get handles GET /api/v1/pops/{name} — get a single POP.
func (h *POPHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	pop, err := h.pops.Get(r.Context(), name)
	if err != nil {
		errorResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pop)
}

// Delete handles DELETE /api/v1/pops/{name} — remove a POP.
func (h *POPHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.pops.Delete(r.Context(), name); err != nil {
		errorResponse(w, err)
		return
	}
	h.logger.Info("pop deleted", "pop", name)
	w.WriteHeader(http.StatusNoContent)
}
