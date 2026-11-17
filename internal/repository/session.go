package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// SessionRepository stores login sessions by the hash of their token.
type SessionRepository struct {
	db *pgxpool.Pool
}

func NewSessionRepository(db *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// FindUser returns the user a valid session belongs to. An unknown token and
// an expired one give the same answer, domain.ErrNotFound.
func (r *SessionRepository) FindUser(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, `
SELECT u.id, u.email, u.name, u.password_hash, u.role, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("find session: %w", err)
	}
	return user, nil
}

// Delete ends one session. Deleting a session that does not exist is not an
// error: the result the caller wants, no session, is already true.
func (r *SessionRepository) Delete(ctx context.Context, tokenHash []byte) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes the sessions that can no longer be used.
func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
