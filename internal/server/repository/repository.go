// Package repository defines data access interfaces for the control plane.
package repository

import (
	"context"

	"github.com/netvantage/netvantage/internal/domain"
)

// AgentRepository manages agent persistence.
type AgentRepository interface {
	Create(ctx context.Context, agent *domain.Agent) error
	Get(ctx context.Context, id string) (*domain.Agent, error)
	List(ctx context.Context) ([]*domain.Agent, error)
	UpdateHeartbeat(ctx context.Context, id string, hb domain.Heartbeat) error
	Delete(ctx context.Context, id string) error
}

// POPRepository manages POP persistence.
type POPRepository interface {
	Create(ctx context.Context, pop *domain.POP) error
	Get(ctx context.Context, name string) (*domain.POP, error)
	List(ctx context.Context) ([]*domain.POP, error)
	Delete(ctx context.Context, name string) error
}

// TestRepository manages test definition persistence.
type TestRepository interface {
	Create(ctx context.Context, test *domain.TestDefinition) error
	Get(ctx context.Context, id string) (*domain.TestDefinition, error)
	List(ctx context.Context) ([]*domain.TestDefinition, error)
	Update(ctx context.Context, test *domain.TestDefinition) error
	Delete(ctx context.Context, id string) error
	ListByPOP(ctx context.Context, popName string) ([]*domain.TestDefinition, error)
}

// TestAssignmentRepository manages test-to-POP assignments.
type TestAssignmentRepository interface {
	Assign(ctx context.Context, testID, popName string) error
	Unassign(ctx context.Context, testID, popName string) error
	ListByTest(ctx context.Context, testID string) ([]*domain.TestAssignment, error)
	ListByPOP(ctx context.Context, popName string) ([]*domain.TestAssignment, error)
}

// APIKeyRepository manages API key persistence.
type APIKeyRepository interface {
	Create(ctx context.Context, key *domain.APIKey) error
	GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error)
	TouchLastUsed(ctx context.Context, id string) error
}
