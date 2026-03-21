// NetVantage Control Plane Server
//
// The server provides the REST API for agent registration, test CRUD,
// config sync, and platform management. Backed by PostgreSQL.
//
// Usage:
//
//	netvantage-server
//	NETVANTAGE_DB_URL=postgres://... netvantage-server
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/server/api/router"
	serverconfig "github.com/netvantage/netvantage/internal/server/config"
	"github.com/netvantage/netvantage/internal/server/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := serverconfig.Load()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Connect to PostgreSQL.
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("database ping failed", "error", err)
		os.Exit(1)
	}
	logger.Info("database connected")

	// Initialize repositories.
	agents := postgres.NewAgentRepo(pool)
	pops := postgres.NewPOPRepo(pool)
	tests := postgres.NewTestRepo(pool)
	assignments := postgres.NewAssignmentRepo(pool)
	apiKeys := postgres.NewAPIKeyRepo(pool)
	audit := postgres.NewAuditRepo(pool)

	// Build router.
	r := router.New(router.Deps{
		Agents:      agents,
		POPs:        pops,
		Tests:       tests,
		Assignments: assignments,
		APIKeys:     apiKeys,
		Audit:       audit,
		Logger:      logger,
	})

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown.
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("control plane server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	logger.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}
	logger.Info("server stopped")
}
