package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// AgentRepo implements repository.AgentRepository against PostgreSQL.
type AgentRepo struct {
	pool *pgxpool.Pool
}

// NewAgentRepo creates a new AgentRepo.
func NewAgentRepo(pool *pgxpool.Pool) *AgentRepo {
	return &AgentRepo{pool: pool}
}

// Create inserts a new agent. Returns ErrAlreadyExists on conflict.
func (r *AgentRepo) Create(ctx context.Context, agent *domain.Agent) error {
	labels, err := json.Marshal(agent.Labels)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO agents (id, pop_name, version, status, capabilities, labels, last_heartbeat, registered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		agent.ID, agent.POPName, agent.Version, string(agent.Status),
		agent.Capabilities, labels, agent.LastHeartbeat, agent.RegisteredAt,
	)
	if err != nil && isDuplicateKey(err) {
		return domain.ErrAlreadyExists
	}
	return err
}

// Get retrieves an agent by ID. Returns ErrNotFound if missing.
func (r *AgentRepo) Get(ctx context.Context, id string) (*domain.Agent, error) {
	a := &domain.Agent{}
	var labelsJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, pop_name, version, status, capabilities, labels,
		       last_heartbeat, registered_at, updated_at
		FROM agents WHERE id = $1`, id,
	).Scan(
		&a.ID, &a.POPName, &a.Version, &a.Status, &a.Capabilities,
		&labelsJSON, &a.LastHeartbeat, &a.RegisteredAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if labelsJSON != nil {
		_ = json.Unmarshal(labelsJSON, &a.Labels)
	}
	return a, nil
}

// List returns all agents.
func (r *AgentRepo) List(ctx context.Context) ([]*domain.Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, pop_name, version, status, capabilities, labels,
		       last_heartbeat, registered_at, updated_at
		FROM agents ORDER BY registered_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*domain.Agent
	for rows.Next() {
		a := &domain.Agent{}
		var labelsJSON []byte
		if err := rows.Scan(
			&a.ID, &a.POPName, &a.Version, &a.Status, &a.Capabilities,
			&labelsJSON, &a.LastHeartbeat, &a.RegisteredAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if labelsJSON != nil {
			_ = json.Unmarshal(labelsJSON, &a.Labels)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// UpdateHeartbeat updates an agent's heartbeat fields.
func (r *AgentRepo) UpdateHeartbeat(ctx context.Context, id string, hb domain.Heartbeat) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE agents SET last_heartbeat = $2, status = $3, version = $4
		WHERE id = $1`,
		id, hb.Timestamp, string(hb.Status), hb.Version,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes an agent by ID.
func (r *AgentRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
