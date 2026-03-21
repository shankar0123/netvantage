// Package router builds the chi router with all API routes.
package router

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/netvantage/netvantage/internal/server/api/handler"
	"github.com/netvantage/netvantage/internal/server/api/middleware"
	"github.com/netvantage/netvantage/internal/server/repository"
)

// Deps holds all dependencies needed to build the router.
type Deps struct {
	Agents      repository.AgentRepository
	POPs        repository.POPRepository
	Tests       repository.TestRepository
	Assignments repository.TestAssignmentRepository
	APIKeys     repository.APIKeyRepository
	Audit       repository.AuditRepository
	Logger      *slog.Logger
}

// New creates a chi router with all API routes registered.
func New(deps Deps) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware.
	rl := middleware.NewRateLimiter(100, time.Second, 200)
	r.Use(rl.Middleware)
	r.Use(middleware.RequestLogger(deps.Logger))

	// Health check — unauthenticated.
	r.Get("/healthz", handler.Healthz)

	// API v1 — authenticated.
	r.Route("/api/v1", func(r chi.Router) {
		auth := middleware.APIKeyAuth(deps.APIKeys, deps.Logger)
		r.Use(auth)

		// Audit logging on all mutations (POST, PUT, DELETE, PATCH).
		if deps.Audit != nil {
			r.Use(middleware.AuditLogger(deps.Audit, deps.Logger))
		}

		// Agents.
		agentH := handler.NewAgentHandler(deps.Agents, deps.POPs, deps.Logger)
		r.Post("/agents", agentH.Register)
		r.Get("/agents", agentH.List)
		r.Get("/agents/{id}", agentH.Get)
		r.Delete("/agents/{id}", agentH.Delete)
		r.Post("/agents/{id}/heartbeat", agentH.Heartbeat)

		// Agent config sync.
		syncH := handler.NewSyncHandler(deps.Tests, deps.Logger)
		r.Get("/agents/{id}/config", syncH.ConfigSync)

		// POPs.
		popH := handler.NewPOPHandler(deps.POPs, deps.Logger)
		r.Post("/pops", popH.Create)
		r.Get("/pops", popH.List)
		r.Get("/pops/{name}", popH.Get)
		r.Delete("/pops/{name}", popH.Delete)

		// Tests.
		testH := handler.NewTestHandler(deps.Tests, deps.Assignments, deps.Logger)
		r.Post("/tests", testH.Create)
		r.Get("/tests", testH.List)
		r.Get("/tests/{id}", testH.Get)
		r.Put("/tests/{id}", testH.Update)
		r.Delete("/tests/{id}", testH.Delete)
		r.Post("/tests/{id}/assign", testH.Assign)

		// Audit log (admin-only read access).
		if deps.Audit != nil {
			auditH := handler.NewAuditHandler(deps.Audit, deps.Logger)
			r.Get("/audit", auditH.List)
		}
	})

	return r
}
