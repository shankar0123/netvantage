package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// AssignmentRepo implements repository.TestAssignmentRepository.
type AssignmentRepo struct {
	pool *pgxpool.Pool
}

// NewAssignmentRepo creates a new AssignmentRepo.
func NewAssignmentRepo(pool *pgxpool.Pool) *AssignmentRepo {
	return &AssignmentRepo{pool: pool}
}

// Assign creates a test-to-POP assignment. Empty popName means global.
func (r *AssignmentRepo) Assign(ctx context.Context, testID, popName string) error {
	var pn *string
	if popName != "" {
		pn = &popName
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO test_assignments (test_id, pop_name)
		VALUES ($1, $2)
		ON CONFLICT (test_id, pop_name) DO NOTHING`,
		testID, pn,
	)
	return err
}

// Unassign removes a test-to-POP assignment.
func (r *AssignmentRepo) Unassign(ctx context.Context, testID, popName string) error {
	var pn *string
	if popName != "" {
		pn = &popName
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM test_assignments WHERE test_id = $1 AND pop_name IS NOT DISTINCT FROM $2`,
		testID, pn,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListByTest returns all assignments for a given test.
func (r *AssignmentRepo) ListByTest(ctx context.Context, testID string) ([]*domain.TestAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, test_id, COALESCE(pop_name, ''), created_at
		FROM test_assignments WHERE test_id = $1
		ORDER BY created_at`, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []*domain.TestAssignment
	for rows.Next() {
		a := &domain.TestAssignment{}
		if err := rows.Scan(&a.ID, &a.TestID, &a.POPName, &a.CreatedAt); err != nil {
			return nil, err
		}
		assignments = append(assignments, a)
	}
	return assignments, rows.Err()
}

// ListByPOP returns all assignments for a given POP.
func (r *AssignmentRepo) ListByPOP(ctx context.Context, popName string) ([]*domain.TestAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, test_id, COALESCE(pop_name, ''), created_at
		FROM test_assignments WHERE pop_name = $1 OR pop_name IS NULL
		ORDER BY created_at`, popName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []*domain.TestAssignment
	for rows.Next() {
		a := &domain.TestAssignment{}
		if err := rows.Scan(&a.ID, &a.TestID, &a.POPName, &a.CreatedAt); err != nil {
			return nil, err
		}
		assignments = append(assignments, a)
	}
	return assignments, rows.Err()
}
