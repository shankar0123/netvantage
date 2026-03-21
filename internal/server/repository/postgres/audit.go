package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// AuditRepo implements repository.AuditRepository using PostgreSQL.
type AuditRepo struct {
	pool *pgxpool.Pool
}

// NewAuditRepo creates a new AuditRepo.
func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo {
	return &AuditRepo{pool: pool}
}

// Record inserts an audit entry into the database.
func (r *AuditRepo) Record(ctx context.Context, entry *domain.AuditEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO audit_log (timestamp, actor_id, actor_role, action, resource, resource_id, source_ip, change_diff)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.Timestamp, entry.ActorID, entry.ActorRole, entry.Action,
		entry.Resource, entry.ResourceID, entry.SourceIP, entry.ChangeDiff,
	)
	if err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	return nil
}

// List returns audit entries ordered by timestamp descending.
func (r *AuditRepo) List(ctx context.Context, limit, offset int) ([]*domain.AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, timestamp, actor_id, actor_role, action, resource, resource_id, source_ip, change_diff
		 FROM audit_log ORDER BY timestamp DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AuditEntry
	for rows.Next() {
		e := &domain.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.ActorID, &e.ActorRole,
			&e.Action, &e.Resource, &e.ResourceID, &e.SourceIP, &e.ChangeDiff); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// ListByResource returns audit entries for a specific resource.
func (r *AuditRepo) ListByResource(ctx context.Context, resource, resourceID string, limit int) ([]*domain.AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, timestamp, actor_id, actor_role, action, resource, resource_id, source_ip, change_diff
		 FROM audit_log WHERE resource = $1 AND resource_id = $2
		 ORDER BY timestamp DESC LIMIT $3`,
		resource, resourceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("audit: list_by_resource: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AuditEntry
	for rows.Next() {
		e := &domain.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.ActorID, &e.ActorRole,
			&e.Action, &e.Resource, &e.ResourceID, &e.SourceIP, &e.ChangeDiff); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
