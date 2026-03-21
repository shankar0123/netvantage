package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/netvantage/netvantage/internal/domain"
)

// APIKeyRepo implements repository.APIKeyRepository.
type APIKeyRepo struct {
	pool *pgxpool.Pool
}

// NewAPIKeyRepo creates a new APIKeyRepo.
func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
	return &APIKeyRepo{pool: pool}
}

// Create inserts a new API key.
func (r *APIKeyRepo) Create(ctx context.Context, key *domain.APIKey) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_keys (id, name, key_hash, role, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		key.ID, key.Name, key.KeyHash, key.Role, key.Scopes, key.ExpiresAt,
	)
	if err != nil && isDuplicateKey(err) {
		return domain.ErrAlreadyExists
	}
	return err
}

// GetByHash retrieves an API key by its hash. Returns ErrNotFound if missing.
func (r *APIKeyRepo) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	k := &domain.APIKey{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, key_hash, role, scopes, created_at, expires_at, last_used
		FROM api_keys WHERE key_hash = $1`, keyHash,
	).Scan(
		&k.ID, &k.Name, &k.KeyHash, &k.Role, &k.Scopes,
		&k.CreatedAt, &k.ExpiresAt, &k.LastUsed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return k, err
}

// TouchLastUsed updates the last_used timestamp for an API key.
func (r *APIKeyRepo) TouchLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used = now() WHERE id = $1`, id)
	return err
}
