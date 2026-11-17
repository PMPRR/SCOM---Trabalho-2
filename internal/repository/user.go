package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// uniqueViolation is the PostgreSQL error code for a broken UNIQUE constraint.
const uniqueViolation = "23505"

// UserRepository reads and writes users. Every value reaches the database
// as a query parameter ($1, $2...), never concatenated into the SQL text,
// which is what makes SQL injection impossible here.
type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `id, email, name, password_hash, role, created_at`

// Create stores a new user. The UNIQUE constraint on email is the authority
// on duplicates: checking first and inserting after would leave a gap for
// two sign-ups with the same address at the same moment.
func (r *UserRepository) Create(ctx context.Context, email, name, passwordHash string, role domain.Role) (domain.User, error) {
	row := r.db.QueryRow(ctx, `
INSERT INTO users (email, name, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING `+userColumns, email, name, passwordHash, string(role))

	user, err := scanUser(row)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.User{}, domain.ErrEmailTaken
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// GetByEmail returns the user with that e-mail, or domain.ErrNotFound.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

// List returns users ordered by name, at most limit of them.
func (r *UserRepository) List(ctx context.Context, limit int) ([]domain.User, error) {
	rows, err := r.db.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY name, id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var (
		user domain.User
		role string
	)
	err := row.Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &role, &user.CreatedAt)
	if err != nil {
		return domain.User{}, err
	}
	user.Role = domain.Role(role)
	return user, nil
}
