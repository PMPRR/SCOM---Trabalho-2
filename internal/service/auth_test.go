package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
)

// memUsers is an in-memory UserRepository.
type memUsers struct {
	byEmail map[string]domain.User
	nextID  int64
}

func (m *memUsers) Create(_ context.Context, email, name, hash string, role domain.Role) (domain.User, error) {
	if _, taken := m.byEmail[email]; taken {
		return domain.User{}, domain.ErrEmailTaken
	}
	m.nextID++
	user := domain.User{ID: m.nextID, Email: email, Name: name, PasswordHash: hash, Role: role, CreatedAt: authNow}
	m.byEmail[email] = user
	return user, nil
}

func (m *memUsers) GetByEmail(_ context.Context, email string) (domain.User, error) {
	user, ok := m.byEmail[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func (m *memUsers) List(context.Context, int) ([]domain.User, error) {
	users := make([]domain.User, 0, len(m.byEmail))
	for _, user := range m.byEmail {
		users = append(users, user)
	}
	return users, nil
}

type memSession struct {
	userID    int64
	expiresAt time.Time
}

// memSessions is an in-memory SessionRepository, keyed by the token hash.
type memSessions struct {
	users    *memUsers
	sessions map[string]memSession
}

func (m *memSessions) Create(_ context.Context, hash []byte, userID int64, expiresAt time.Time) error {
	m.sessions[string(hash)] = memSession{userID: userID, expiresAt: expiresAt}
	return nil
}

func (m *memSessions) FindUser(_ context.Context, hash []byte, now time.Time) (domain.User, error) {
	session, ok := m.sessions[string(hash)]
	if !ok || !session.expiresAt.After(now) {
		return domain.User{}, domain.ErrNotFound
	}
	for _, user := range m.users.byEmail {
		if user.ID == session.userID {
			return user, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memSessions) Delete(_ context.Context, hash []byte) error {
	delete(m.sessions, string(hash))
	return nil
}

func (m *memSessions) DeleteExpired(_ context.Context, now time.Time) error {
	for hash, session := range m.sessions {
		if !session.expiresAt.After(now) {
			delete(m.sessions, hash)
		}
	}
	return nil
}

var authNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

type authFixture struct {
	svc      *AuthService
	users    *memUsers
	sessions *memSessions
	now      *time.Time
}

func newAuthFixture(t *testing.T) authFixture {
	t.Helper()
	users := &memUsers{byEmail: map[string]domain.User{}}
	sessions := &memSessions{users: users, sessions: map[string]memSession{}}
	// Cost 4 is the cheapest bcrypt allows: these tests check behaviour.
	svc, err := NewAuthService(users, sessions, security.Bcrypt{Cost: 4}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	now := authNow
	svc.now = func() time.Time { return now }
	return authFixture{svc: svc, users: users, sessions: sessions, now: &now}
}

func validRegistration() RegisterInput {
	return RegisterInput{
		Name: "Ana Souza", Email: "Ana@Example.com",
		Password: "senha-bem-longa", PasswordConfirm: "senha-bem-longa", AcceptedTerms: true,
	}
}

func TestRegisterCreatesOperatorWithHashedPassword(t *testing.T) {
	f := newAuthFixture(t)

	user, err := f.svc.Register(context.Background(), validRegistration())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != domain.RoleOperator {
		t.Errorf("role = %q, want operator: a form must never create an administrator", user.Role)
	}
	if user.Email != "ana@example.com" {
		t.Errorf("email = %q, want it stored in lower case", user.Email)
	}
	if user.PasswordHash == "" || strings.Contains(user.PasswordHash, "senha-bem-longa") {
		t.Errorf("password stored as %q, want a hash", user.PasswordHash)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		change func(*RegisterInput)
		field  string
	}{
		{"empty name", func(in *RegisterInput) { in.Name = "   " }, "nome"},
		{"name too long", func(in *RegisterInput) { in.Name = strings.Repeat("a", 101) }, "nome"},
		{"email without domain", func(in *RegisterInput) { in.Email = "ana" }, "email"},
		{"email without suffix", func(in *RegisterInput) { in.Email = "ana@localhost" }, "email"},
		{"email with display name", func(in *RegisterInput) { in.Email = "Ana <ana@example.com>" }, "email"},
		{"email as SQL", func(in *RegisterInput) { in.Email = "' OR '1'='1" }, "email"},
		{"short password", func(in *RegisterInput) { in.Password, in.PasswordConfirm = "curta", "curta" }, "senha"},
		{"password beyond bcrypt", func(in *RegisterInput) {
			in.Password = strings.Repeat("a", 73)
			in.PasswordConfirm = in.Password
		}, "senha"},
		{"confirmation differs", func(in *RegisterInput) { in.PasswordConfirm = "outra-senha-longa" }, "senha-confirma"},
		{"terms not accepted", func(in *RegisterInput) { in.AcceptedTerms = false }, "termos"},
	}
	for _, c := range cases {
		f := newAuthFixture(t)
		in := validRegistration()
		c.change(&in)

		_, err := f.svc.Register(context.Background(), in)

		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want ValidationError", c.name, err)
			continue
		}
		if invalid.Field != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, invalid.Field, c.field)
		}
		if len(f.users.byEmail) != 0 {
			t.Errorf("%s: an invalid registration reached the database", c.name)
		}
	}
}

func TestRegisterRejectsDuplicateEmailWhateverTheCase(t *testing.T) {
	f := newAuthFixture(t)
	if _, err := f.svc.Register(context.Background(), validRegistration()); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	again := validRegistration()
	again.Email = "ANA@EXAMPLE.COM"
	if _, err := f.svc.Register(context.Background(), again); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("err = %v, want ErrEmailTaken", err)
	}
}

func TestLoginOpensSessionThatAuthenticates(t *testing.T) {
	f := newAuthFixture(t)
	registered, _ := f.svc.Register(context.Background(), validRegistration())

	session, err := f.svc.Login(context.Background(), " ana@example.com ", "senha-bem-longa", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Token == "" || session.User.ID != registered.ID || session.Persistent {
		t.Errorf("session = %+v", session)
	}
	if want := authNow.Add(sessionDuration); !session.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want %v", session.ExpiresAt, want)
	}
	// The database must hold the hash of the token, never the token itself.
	if _, stored := f.sessions.sessions[session.Token]; stored {
		t.Error("the session token was stored in clear")
	}

	user, err := f.svc.Authenticate(context.Background(), session.Token)
	if err != nil || user.ID != registered.ID {
		t.Errorf("Authenticate: user = %+v, err = %v", user, err)
	}
}

func TestLoginRememberedLastsLonger(t *testing.T) {
	f := newAuthFixture(t)
	f.svc.Register(context.Background(), validRegistration())

	session, err := f.svc.Login(context.Background(), "ana@example.com", "senha-bem-longa", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !session.Persistent || !session.ExpiresAt.Equal(authNow.Add(rememberedDuration)) {
		t.Errorf("session = %+v, want a persistent 30-day session", session)
	}
}

func TestLoginGivesTheSameErrorForUnknownEmailAndWrongPassword(t *testing.T) {
	f := newAuthFixture(t)
	f.svc.Register(context.Background(), validRegistration())

	_, wrongPassword := f.svc.Login(context.Background(), "ana@example.com", "senha-errada-aqui", false)
	_, unknownEmail := f.svc.Login(context.Background(), "ninguem@example.com", "senha-bem-longa", false)
	_, injection := f.svc.Login(context.Background(), "' OR '1'='1' --", "x", false)

	for name, err := range map[string]error{"wrong password": wrongPassword, "unknown email": unknownEmail, "SQL in the email": injection} {
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("%s: err = %v, want ErrInvalidCredentials", name, err)
		}
	}
	if len(f.sessions.sessions) != 0 {
		t.Error("a failed login created a session")
	}
}

func TestAuthenticateRejectsMissingForgedAndExpiredTokens(t *testing.T) {
	f := newAuthFixture(t)
	f.svc.Register(context.Background(), validRegistration())
	session, _ := f.svc.Login(context.Background(), "ana@example.com", "senha-bem-longa", false)

	for name, token := range map[string]string{"empty": "", "forged": "token-inventado", "almost right": session.Token + "x"} {
		if _, err := f.svc.Authenticate(context.Background(), token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%s token: err = %v, want ErrUnauthenticated", name, err)
		}
	}

	*f.now = authNow.Add(sessionDuration) // the exact moment of expiry
	if _, err := f.svc.Authenticate(context.Background(), session.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired session: err = %v, want ErrUnauthenticated", err)
	}
}

func TestLogoutEndsTheSessionForGood(t *testing.T) {
	f := newAuthFixture(t)
	f.svc.Register(context.Background(), validRegistration())
	session, _ := f.svc.Login(context.Background(), "ana@example.com", "senha-bem-longa", false)

	if err := f.svc.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Reusing the token after logout is the classic session replay.
	if _, err := f.svc.Authenticate(context.Background(), session.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("token still valid after logout: err = %v", err)
	}
	if err := f.svc.Logout(context.Background(), session.Token); err != nil {
		t.Errorf("logging out twice should not fail: %v", err)
	}
}

func TestLoginClearsExpiredSessions(t *testing.T) {
	f := newAuthFixture(t)
	f.svc.Register(context.Background(), validRegistration())
	f.svc.Login(context.Background(), "ana@example.com", "senha-bem-longa", false)

	*f.now = authNow.Add(sessionDuration + time.Hour)
	f.svc.Login(context.Background(), "ana@example.com", "senha-bem-longa", false)

	if n := len(f.sessions.sessions); n != 1 {
		t.Errorf("%d sessions stored, want only the new one", n)
	}
}

func TestListUsersIsForAdministratorsOnly(t *testing.T) {
	f := newAuthFixture(t)
	operator, _ := f.svc.Register(context.Background(), validRegistration())

	if _, err := f.svc.ListUsers(context.Background(), operator); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("operator: err = %v, want ErrForbidden", err)
	}

	admin := operator
	admin.Role = domain.RoleAdmin
	users, err := f.svc.ListUsers(context.Background(), admin)
	if err != nil || len(users) != 1 {
		t.Errorf("admin: %d users, err = %v", len(users), err)
	}
}
