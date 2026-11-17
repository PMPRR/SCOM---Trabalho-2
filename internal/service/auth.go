package service

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
)

const (
	minPasswordLength = 8
	maxNameLength     = 100 // the limit of users.name
	maxEmailLength    = 254 // the limit of users.email

	// A session lasts until the browser closes, with this ceiling on the
	// server; "keep me signed in" trades that for a long-lived cookie.
	sessionDuration    = 12 * time.Hour
	rememberedDuration = 30 * 24 * time.Hour

	maxListedUsers = 500
)

// UserRepository is what authentication needs from the users table.
type UserRepository interface {
	Create(ctx context.Context, email, name, passwordHash string, role domain.Role) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	List(ctx context.Context, limit int) ([]domain.User, error)
}

// SessionRepository is what authentication needs from the sessions table.
type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error
	FindUser(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error)
	Delete(ctx context.Context, tokenHash []byte) error
	DeleteExpired(ctx context.Context, now time.Time) error
}

// PasswordHasher turns a password into what is stored and checks it later.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

// RegisterInput is the sign-up form as it arrives, before any check.
type RegisterInput struct {
	Name            string
	Email           string
	Password        string
	PasswordConfirm string
	AcceptedTerms   bool
}

// Session is a login that was just created. Token goes to the browser in a
// cookie and is never stored: the database keeps only its hash.
type Session struct {
	Token     string
	ExpiresAt time.Time
	// Persistent tells whether the cookie should outlive the browser window.
	Persistent bool
	User       domain.User
}

// AuthService registers users, logs them in and out, and recognizes the
// user behind a session token. It writes the audit lines of authentication:
// account created, login succeeded, authentication failed.
type AuthService struct {
	users    UserRepository
	sessions SessionRepository
	hasher   PasswordHasher
	log      *slog.Logger
	now      func() time.Time
	newToken func() (string, error)

	// decoyHash is checked when the e-mail has no account, so that a login
	// takes the same time whether or not the account exists.
	decoyHash string
}

func NewAuthService(users UserRepository, sessions SessionRepository, hasher PasswordHasher, log *slog.Logger) (*AuthService, error) {
	decoy, err := hasher.Hash("decoy-password-never-used")
	if err != nil {
		return nil, err
	}
	return &AuthService{
		users: users, sessions: sessions, hasher: hasher, log: log,
		now: time.Now, newToken: security.NewToken, decoyHash: decoy,
	}, nil
}

// Register validates the form and creates the account. Every new account is
// an operator: an administrator is promoted on the server, never by a form.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (domain.User, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return domain.User{}, &domain.ValidationError{Field: "nome", Message: "Informe um nome de até 100 caracteres."}
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return domain.User{}, err
	}
	if utf8.RuneCountInString(in.Password) < minPasswordLength {
		return domain.User{}, &domain.ValidationError{Field: "senha", Message: "A senha precisa ter no mínimo 8 caracteres."}
	}
	if len(in.Password) > security.MaxPasswordBytes {
		return domain.User{}, &domain.ValidationError{Field: "senha", Message: "A senha pode ter no máximo 72 caracteres."}
	}
	if in.Password != in.PasswordConfirm {
		return domain.User{}, &domain.ValidationError{Field: "senha-confirma", Message: "As senhas não são iguais."}
	}
	if !in.AcceptedTerms {
		return domain.User{}, &domain.ValidationError{Field: "termos", Message: "É preciso concordar que as estações cadastradas ficam públicas."}
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return domain.User{}, err
	}
	user, err := s.users.Create(ctx, email, name, hash, domain.RoleOperator)
	if err != nil {
		if errors.Is(err, domain.ErrEmailTaken) {
			s.log.Warn("registration refused", "reason", "email already registered")
		}
		return domain.User{}, err
	}
	s.log.Info("user created", "user_id", user.ID, "role", string(user.Role))
	return user, nil
}

// Login checks the credentials and opens a session.
func (s *AuthService) Login(ctx context.Context, email, password string, remember bool) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		s.hasher.Verify(s.decoyHash, password)
		s.log.Warn("authentication failed", "reason", "unknown email")
		return Session{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if !s.hasher.Verify(user.PasswordHash, password) {
		s.log.Warn("authentication failed", "reason", "wrong password", "user_id", user.ID)
		return Session{}, domain.ErrInvalidCredentials
	}

	session, err := s.OpenSession(ctx, user, remember)
	if err != nil {
		return Session{}, err
	}
	s.log.Info("login succeeded", "user_id", user.ID, "role", string(user.Role))
	return session, nil
}

// OpenSession creates a session for a user whose identity is already
// established: after a login, or right after signing up.
func (s *AuthService) OpenSession(ctx context.Context, user domain.User, remember bool) (Session, error) {
	token, err := s.newToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	duration := sessionDuration
	if remember {
		duration = rememberedDuration
	}
	expiresAt := now.Add(duration)

	// Housekeeping rides on logins: no background job is needed to keep the
	// table from accumulating dead sessions.
	if err := s.sessions.DeleteExpired(ctx, now); err != nil {
		return Session{}, err
	}
	if err := s.sessions.Create(ctx, security.HashToken(token), user.ID, expiresAt); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expiresAt, Persistent: remember, User: user}, nil
}

// Logout ends the session of a token. An empty or unknown token is not an
// error: there is nothing left to end.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, security.HashToken(token))
}

// Authenticate returns the user behind a session token, or
// domain.ErrUnauthenticated when the token is missing, unknown or expired.
func (s *AuthService) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, domain.ErrUnauthenticated
	}
	user, err := s.sessions.FindUser(ctx, security.HashToken(token), s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return user, err
}

// ListUsers returns the accounts, for the administration page. Only an
// administrator may call it; the check is repeated here so the rule holds
// even if a route is wired without its middleware.
func (s *AuthService) ListUsers(ctx context.Context, actor domain.User) ([]domain.User, error) {
	if !actor.IsAdmin() {
		return nil, domain.ErrForbidden
	}
	return s.users.List(ctx, maxListedUsers)
}

// normalizeEmail accepts a bare address ("ana@example.com") and returns it
// in lower case, the form it is stored and looked up in.
func normalizeEmail(raw string) (string, error) {
	invalid := &domain.ValidationError{Field: "email", Message: "Informe um e-mail válido."}

	email := strings.TrimSpace(raw)
	if len(email) < 3 || len(email) > maxEmailLength {
		return "", invalid
	}
	parsed, err := mail.ParseAddress(email)
	// ParseAddress also accepts "Ana <ana@example.com>"; only the bare
	// address is an e-mail for this form.
	if err != nil || parsed.Address != email {
		return "", invalid
	}
	// A public sign-up has no use for addresses without a domain suffix,
	// such as "ana@localhost".
	if domainPart := email[strings.LastIndex(email, "@")+1:]; !strings.Contains(domainPart, ".") {
		return "", invalid
	}
	return strings.ToLower(email), nil
}
