package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// TestRepo implements repository.TestRepository against PostgreSQL.
type TestRepo struct {
	pool *pgxpool.Pool
}

// NewTestRepo creates a new TestRepo.
func NewTestRepo(pool *pgxpool.Pool) *TestRepo {
	return &TestRepo{pool: pool}
}

// Create inserts a new test definition.
func (r *TestRepo) Create(ctx context.Context, test *domain.TestDefinition) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO test_definitions (id, name, test_type, target, interval_ms, timeout_ms, config, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		test.ID, test.Name, test.TestType, test.Target,
		test.IntervalMS, test.TimeoutMS, test.Config, test.Enabled,
	)
	if err != nil && isDuplicateKey(err) {
		return domain.ErrAlreadyExists
	}
	return err
}

// Get retrieves a test definition by ID.
func (r *TestRepo) Get(ctx context.Context, id string) (*domain.TestDefinition, error) {
	t := &domain.TestDefinition{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, test_type, target, interval_ms, timeout_ms, config, enabled,
		       created_at, updated_at
		FROM test_definitions WHERE id = $1`, id,
	).Scan(
		&t.ID, &t.Name, &t.TestType, &t.Target,
		&t.IntervalMS, &t.TimeoutMS, &t.Config, &t.Enabled,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

// List returns all test definitions.
func (r *TestRepo) List(ctx context.Context) ([]*domain.TestDefinition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, test_type, target, interval_ms, timeout_ms, config, enabled,
		       created_at, updated_at
		FROM test_definitions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tests []*domain.TestDefinition
	for rows.Next() {
		t := &domain.TestDefinition{}
		if err := rows.Scan(
			&t.ID, &t.Name, &t.TestType, &t.Target,
			&t.IntervalMS, &t.TimeoutMS, &t.Config, &t.Enabled,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tests = append(tests, t)
	}
	return tests, rows.Err()
}

// Update updates a test definition's mutable fields.
func (r *TestRepo) Update(ctx context.Context, test *domain.TestDefinition) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE test_definitions
		SET name = $2, test_type = $3, target = $4, interval_ms = $5,
		    timeout_ms = $6, config = $7, enabled = $8
		WHERE id = $1`,
		test.ID, test.Name, test.TestType, test.Target,
		test.IntervalMS, test.TimeoutMS, test.Config, test.Enabled,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes a test definition by ID.
func (r *TestRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM test_definitions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListByPOP returns enabled test definitions assigned to a specific POP,
// including global tests (those with NULL pop_name in test_assignments).
func (r *TestRepo) ListByPOP(ctx context.Context, popName string) ([]*domain.TestDefinition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT td.id, td.name, td.test_type, td.target,
		       td.interval_ms, td.timeout_ms, td.config, td.enabled,
		       td.created_at, td.updated_at
		FROM test_definitions td
		JOIN test_assignments ta ON td.id = ta.test_id
		WHERE td.enabled = true
		  AND (ta.pop_name = $1 OR ta.pop_name IS NULL)
		ORDER BY td.created_at DESC`, popName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tests []*domain.TestDefinition
	for rows.Next() {
		t := &domain.TestDefinition{}
		if err := rows.Scan(
			&t.ID, &t.Name, &t.TestType, &t.Target,
			&t.IntervalMS, &t.TimeoutMS, &t.Config, &t.Enabled,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tests = append(tests, t)
	}
	return tests, rows.Err()
}
