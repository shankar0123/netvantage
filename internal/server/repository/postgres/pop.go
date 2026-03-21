package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// POPRepo implements repository.POPRepository against PostgreSQL.
type POPRepo struct {
	pool *pgxpool.Pool
}

// NewPOPRepo creates a new POPRepo.
func NewPOPRepo(pool *pgxpool.Pool) *POPRepo {
	return &POPRepo{pool: pool}
}

// Create inserts a new POP. Returns ErrAlreadyExists on conflict.
func (r *POPRepo) Create(ctx context.Context, pop *domain.POP) error {
	labels, err := json.Marshal(pop.Labels)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO pops (name, provider, city, country, latitude, longitude, labels)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		pop.Name, pop.Provider, pop.City, pop.Country,
		pop.Latitude, pop.Longitude, labels,
	)
	if err != nil && isDuplicateKey(err) {
		return domain.ErrAlreadyExists
	}
	return err
}

// Get retrieves a POP by name. Returns ErrNotFound if missing.
func (r *POPRepo) Get(ctx context.Context, name string) (*domain.POP, error) {
	p := &domain.POP{}
	var labelsJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT name, provider, city, country, latitude, longitude, labels,
		       created_at, updated_at
		FROM pops WHERE name = $1`, name,
	).Scan(
		&p.Name, &p.Provider, &p.City, &p.Country,
		&p.Latitude, &p.Longitude, &labelsJSON,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if labelsJSON != nil {
		_ = json.Unmarshal(labelsJSON, &p.Labels)
	}
	return p, nil
}

// List returns all POPs.
func (r *POPRepo) List(ctx context.Context) ([]*domain.POP, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, provider, city, country, latitude, longitude, labels,
		       created_at, updated_at
		FROM pops ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pops []*domain.POP
	for rows.Next() {
		p := &domain.POP{}
		var labelsJSON []byte
		if err := rows.Scan(
			&p.Name, &p.Provider, &p.City, &p.Country,
			&p.Latitude, &p.Longitude, &labelsJSON,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if labelsJSON != nil {
			_ = json.Unmarshal(labelsJSON, &p.Labels)
		}
		pops = append(pops, p)
	}
	return pops, rows.Err()
}

// Delete removes a POP by name.
func (r *POPRepo) Delete(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM pops WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
