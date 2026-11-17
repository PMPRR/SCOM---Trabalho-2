package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// The tests below run the real handlers, middlewares, service and templates
// over HTTP, with a cookie jar, the way a browser would. Only the database
// is replaced, by the two in-memory stores that follow.

type memUsers struct {
	byEmail map[string]domain.User
	nextID  int64
}

func (m *memUsers) Create(_ context.Context, email, name, hash string, role domain.Role) (domain.User, error) {
	if _, taken := m.byEmail[email]; taken {
		return domain.User{}, domain.ErrEmailTaken
	}
	m.nextID++
	user := domain.User{ID: m.nextID, Email: email, Name: name, PasswordHash: hash, Role: role, CreatedAt: time.Now()}
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

type memSessions struct {
	users    *memUsers
	sessions map[string]int64 // token hash -> user id
}

func (m *memSessions) Create(_ context.Context, hash []byte, userID int64, _ time.Time) error {
	m.sessions[string(hash)] = userID
	return nil
}

func (m *memSessions) FindUser(_ context.Context, hash []byte, _ time.Time) (domain.User, error) {
	userID, ok := m.sessions[string(hash)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	for _, user := range m.users.byEmail {
		if user.ID == userID {
			return user, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memSessions) Delete(_ context.Context, hash []byte) error {
	delete(m.sessions, string(hash))
	return nil
}

func (m *memSessions) DeleteExpired(context.Context, time.Time) error { return nil }

// site is the application running on a test server, plus a browser.
type site struct {
	t      *testing.T
	url    string
	users  *memUsers
	client *http.Client
}

func newSite(t *testing.T) *site {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rd := render.New(log, false)
	if err := rd.LoadTemplates(os.DirFS("../../../web/templates")); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	rd.UseErrorPage(ErrorPageData)
	users := &memUsers{byEmail: map[string]domain.User{}}
	sessions := &memSessions{users: users, sessions: map[string]int64{}}
	authService, err := service.NewAuthService(users, sessions, security.Bcrypt{Cost: 4}, log)
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}

	auth := NewAuthHandler(authService, rd, false)
	pages := NewPageHandler(rd)
	throttled := middleware.RateLimit(security.NewRateLimiter(10, time.Minute), rd)

	// The same routes and the same middleware order as server.NewRouter.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", pages.Home)
	mux.HandleFunc("GET /entrar", auth.LoginPage)
	mux.HandleFunc("GET /cadastro", auth.SignupPage)
	mux.Handle("POST /entrar", throttled(http.HandlerFunc(auth.Login)))
	mux.Handle("POST /cadastro", throttled(http.HandlerFunc(auth.Signup)))
	mux.HandleFunc("POST /sair", auth.Logout)
	mux.Handle("GET /conta", middleware.RequireAuth(rd)(http.HandlerFunc(auth.Account)))
	mux.Handle("GET /admin/usuarios", middleware.RequireRole(domain.RoleAdmin, rd)(http.HandlerFunc(auth.Users)))

	var root http.Handler = mux
	root = middleware.LoadUser(authService, rd)(root)
	root = middleware.CSRF(rd, false)(root)

	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)
	return &site{t: t, url: srv.URL, users: users, client: newBrowser(t)}
}

// newBrowser keeps cookies between requests and does not follow redirects,
// so the tests can look at them.
func newBrowser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type reply struct {
	status  int
	header  http.Header
	body    string
	cookies []*http.Cookie
}

func (s *site) do(req *http.Request) reply {
	s.t.Helper()
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, body: string(body), cookies: resp.Cookies()}
}

func (s *site) get(path string, htmx bool) reply {
	s.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, s.url+path, nil)
	req.Header.Set("Accept", "text/html")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return s.do(req)
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// csrf loads a page with a form and returns the token it carries.
func (s *site) csrf() string {
	s.t.Helper()
	page := s.get("/entrar", false)
	if page.status == http.StatusSeeOther { // already logged in: any page has the logout form
		page = s.get("/conta", false)
	}
	match := csrfField.FindStringSubmatch(page.body)
	if match == nil {
		s.t.Fatalf("no CSRF field in the page (status %d)", page.status)
	}
	return match[1]
}

// post sends a form. htmx sends it the way HTMX does: token in the header.
func (s *site) post(path string, form url.Values, htmx bool) reply {
	s.t.Helper()
	token := s.csrf()
	if !htmx {
		form.Set("csrf_token", token)
	}
	req, _ := http.NewRequest(http.MethodPost, s.url+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("X-CSRF-Token", token)
	}
	return s.do(req)
}

func signupForm(name, email string) url.Values {
	return url.Values{
		"nome": {name}, "email": {email},
		"senha": {"senha-bem-longa"}, "senha-confirma": {"senha-bem-longa"}, "termos": {"on"},
	}
}

func loginForm(email, password string) url.Values {
	return url.Values{"email": {email}, "senha": {password}}
}

func (s *site) signup(name, email string) {
	s.t.Helper()
	if r := s.post("/cadastro", signupForm(name, email), true); r.status != http.StatusNoContent {
		s.t.Fatalf("signup: status %d, body %s", r.status, r.body)
	}
}

func (s *site) promote(email string) {
	user := s.users.byEmail[email]
	user.Role = domain.RoleAdmin
	s.users.byEmail[email] = user
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestSignupLogsInAndOpensTheAccountPage(t *testing.T) {
	s := newSite(t)

	r := s.post("/cadastro", signupForm("Ana Souza", "ana@example.com"), true)

	if r.status != http.StatusNoContent || r.header.Get("HX-Redirect") != "/conta" {
		t.Fatalf("status = %d, HX-Redirect = %q; want 204 and /conta", r.status, r.header.Get("HX-Redirect"))
	}
	session := cookieNamed(r.cookies, middleware.SessionCookie)
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v, want HttpOnly and SameSite=Lax", session)
	}
	if session.MaxAge != 0 || !session.Expires.IsZero() {
		t.Errorf("session cookie should end with the browser window: %+v", session)
	}

	account := s.get("/conta", false)
	if account.status != http.StatusOK {
		t.Fatalf("/conta: status %d", account.status)
	}
	for _, piece := range []string{"Ana Souza", "ana@example.com", "Operador", `action="/sair"`} {
		if !strings.Contains(account.body, piece) {
			t.Errorf("/conta lacks %q", piece)
		}
	}
	if strings.Contains(account.body, `href="/admin/usuarios"`) {
		t.Error("an operator is offered the administration page")
	}
}

func TestStateChangingRequestsNeedTheCSRFToken(t *testing.T) {
	s := newSite(t)
	s.csrf() // the browser now holds the CSRF cookie

	send := func(mutate func(*http.Request, url.Values)) reply {
		form := signupForm("Ana", "ana@example.com")
		req, _ := http.NewRequest(http.MethodPost, s.url+"/cadastro", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mutate(req, form)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		return s.do(req)
	}

	cases := map[string]func(*http.Request, url.Values){
		"no token at all":       func(*http.Request, url.Values) {},
		"wrong token in header": func(r *http.Request, _ url.Values) { r.Header.Set("X-CSRF-Token", "token-de-outro-site") },
		"wrong token in field":  func(_ *http.Request, f url.Values) { f.Set("csrf_token", "token-de-outro-site") },
	}
	for name, mutate := range cases {
		r := send(mutate)
		if r.status != http.StatusForbidden || !strings.Contains(r.body, "csrf_failed") {
			t.Errorf("%s: status = %d, body = %s; want 403 csrf_failed", name, r.status, r.body)
		}
	}
	if len(s.users.byEmail) != 0 {
		t.Error("a request without a valid CSRF token created an account")
	}
}

func TestLoginWithWrongPassword(t *testing.T) {
	s := newSite(t)
	s.signup("Ana Souza", "ana@example.com")
	other := &site{t: t, url: s.url, users: s.users, client: newBrowser(t)} // a second browser

	htmx := other.post("/entrar", loginForm("ana@example.com", "senha-errada-aqui"), true)
	if htmx.status != http.StatusUnauthorized {
		t.Errorf("HTMX: status = %d, want 401", htmx.status)
	}
	if !strings.Contains(htmx.body, `class="request-error"`) || !strings.Contains(htmx.body, "E-mail ou senha incorretos.") {
		t.Errorf("HTMX: body = %s", htmx.body)
	}
	if strings.Contains(htmx.body, "<html") {
		t.Error("HTMX should receive a fragment, not the page")
	}

	plain := other.post("/entrar", loginForm("ana@example.com", "senha-errada-aqui"), false)
	if plain.status != http.StatusUnauthorized || !strings.Contains(plain.body, "<html") {
		t.Fatalf("plain form: status = %d, want 401 with the page", plain.status)
	}
	if !strings.Contains(plain.body, "E-mail ou senha incorretos.") || !strings.Contains(plain.body, `value="ana@example.com"`) {
		t.Error("the page should come back with the message and the e-mail filled in")
	}
	if strings.Contains(plain.body, "senha-errada-aqui") {
		t.Error("the page echoes the password that was typed")
	}
	if cookieNamed(plain.cookies, middleware.SessionCookie) != nil {
		t.Error("a failed login set a session cookie")
	}
}

func TestLoginAndRememberMe(t *testing.T) {
	s := newSite(t)
	s.signup("Ana Souza", "ana@example.com")
	other := &site{t: t, url: s.url, users: s.users, client: newBrowser(t)}

	form := loginForm("ana@example.com", "senha-bem-longa")
	form.Set("lembrar", "on")
	r := other.post("/entrar", form, false)

	if r.status != http.StatusSeeOther || r.header.Get("Location") != "/conta" {
		t.Fatalf("status = %d, Location = %q; want 303 to /conta", r.status, r.header.Get("Location"))
	}
	session := cookieNamed(r.cookies, middleware.SessionCookie)
	if session == nil || session.MaxAge < 29*24*3600 {
		t.Errorf("session cookie = %+v, want one that lasts about 30 days", session)
	}
	if other.get("/conta", false).status != http.StatusOK {
		t.Error("the logged-in browser cannot open /conta")
	}
	if r := other.get("/entrar", false); r.status != http.StatusSeeOther {
		t.Errorf("/entrar while logged in: status = %d, want a redirect to the account", r.status)
	}
}

func TestProtectedPageWithoutLogin(t *testing.T) {
	s := newSite(t)

	browser := s.get("/conta", false)
	if browser.status != http.StatusSeeOther || browser.header.Get("Location") != "/entrar" {
		t.Errorf("browser: status = %d, Location = %q; want 303 to /entrar", browser.status, browser.header.Get("Location"))
	}

	htmx := s.get("/conta", true)
	if htmx.status != http.StatusUnauthorized || htmx.header.Get("HX-Redirect") != "/entrar" {
		t.Errorf("HTMX: status = %d, HX-Redirect = %q; want 401 and /entrar", htmx.status, htmx.header.Get("HX-Redirect"))
	}

	// The same request, now carrying an invented session token.
	req, _ := http.NewRequest(http.MethodGet, s.url+"/conta", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: "sessao-inventada"})
	if forged := s.do(req); forged.status != http.StatusSeeOther {
		t.Errorf("forged session cookie: status = %d, want 303 to the login", forged.status)
	}
}

func TestAdminPageIsForAdministratorsOnly(t *testing.T) {
	s := newSite(t)
	s.signup("Ana Souza", "ana@example.com")

	denied := s.get("/admin/usuarios", false)
	if denied.status != http.StatusForbidden {
		t.Errorf("operator: status = %d, want 403", denied.status)
	}
	// Someone browsing gets a page of the site, still logged in, not JSON.
	for _, piece := range []string{"<html", "Você não tem permissão para acessar isto.", `action="/sair"`} {
		if !strings.Contains(denied.body, piece) {
			t.Errorf("the 403 page lacks %q", piece)
		}
	}
	if strings.Contains(denied.body, "ana@example.com") && strings.Contains(denied.body, "users-table") {
		t.Error("the 403 page shows the user list")
	}

	s.promote("ana@example.com")
	r := s.get("/admin/usuarios", false)
	if r.status != http.StatusOK || !strings.Contains(r.body, "ana@example.com") || !strings.Contains(r.body, "Administrador") {
		t.Errorf("admin: status = %d, body lacks the user list", r.status)
	}
	if strings.Contains(r.body, "$2a$") || strings.Contains(r.body, "$2b$") {
		t.Error("the user list shows password hashes")
	}
	if !strings.Contains(s.get("/conta", false).body, `href="/admin/usuarios"`) {
		t.Error("an administrator is not offered the administration page")
	}

	visitor := &site{t: t, url: s.url, users: s.users, client: newBrowser(t)}
	if r := visitor.get("/admin/usuarios", false); r.status != http.StatusSeeOther {
		t.Errorf("visitor: status = %d, want 303 to the login", r.status)
	}
}

func TestLogoutEndsTheSessionEvenIfTheCookieIsReplayed(t *testing.T) {
	s := newSite(t)
	r := s.post("/cadastro", signupForm("Ana Souza", "ana@example.com"), true)
	stolen := cookieNamed(r.cookies, middleware.SessionCookie)

	out := s.post("/sair", url.Values{}, false)
	if out.status != http.StatusSeeOther || out.header.Get("Location") != "/" {
		t.Fatalf("logout: status = %d, Location = %q", out.status, out.header.Get("Location"))
	}
	if cleared := cookieNamed(out.cookies, middleware.SessionCookie); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("logout should expire the session cookie: %+v", cleared)
	}
	if r := s.get("/conta", false); r.status != http.StatusSeeOther {
		t.Errorf("/conta after logout: status = %d, want 303", r.status)
	}

	// Someone who copied the cookie before the logout tries to use it.
	req, _ := http.NewRequest(http.MethodGet, s.url+"/conta", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(stolen)
	replay := (&site{t: t, url: s.url, users: s.users, client: newBrowser(t)}).do(req)
	if replay.status != http.StatusSeeOther {
		t.Errorf("replayed session: status = %d, want 303 to the login", replay.status)
	}
}

func TestSignupRefusals(t *testing.T) {
	s := newSite(t)
	s.signup("Ana Souza", "ana@example.com")
	other := &site{t: t, url: s.url, users: s.users, client: newBrowser(t)}

	duplicate := other.post("/cadastro", signupForm("Outra Ana", "ANA@example.com"), true)
	if duplicate.status != http.StatusConflict || !strings.Contains(duplicate.body, "Já existe uma conta com este e-mail.") {
		t.Errorf("duplicate e-mail: status = %d, body = %s", duplicate.status, duplicate.body)
	}

	short := signupForm("Bia", "bia@example.com")
	short.Set("senha", "curta")
	short.Set("senha-confirma", "curta")
	plain := other.post("/cadastro", short, false)
	if plain.status != http.StatusBadRequest || !strings.Contains(plain.body, "A senha precisa ter no mínimo 8 caracteres.") {
		t.Errorf("short password: status = %d", plain.status)
	}
	if !strings.Contains(plain.body, `value="bia@example.com"`) || strings.Contains(plain.body, "curta") {
		t.Error("the page should keep the e-mail and never echo the password")
	}
}

func TestUserNameIsEscapedOnThePage(t *testing.T) {
	s := newSite(t)
	s.signup(`<script>alert("xss")</script>`, "ana@example.com")

	body := s.get("/conta", false).body
	if strings.Contains(body, `<script>alert(`) {
		t.Error("the user's name reached the page as HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("the escaped name was not found on the page")
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	s := newSite(t)

	var last reply
	for i := 0; i < 11; i++ {
		last = s.post("/entrar", loginForm("ana@example.com", "tentativa-de-senha"), true)
		if i < 10 && last.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, last.status)
		}
	}
	if last.status != http.StatusTooManyRequests || last.header.Get("Retry-After") == "" {
		t.Errorf("11th attempt: status = %d, Retry-After = %q; want 429 with Retry-After", last.status, last.header.Get("Retry-After"))
	}
	if !strings.Contains(last.body, "Muitas tentativas.") {
		t.Errorf("body = %s", last.body)
	}
}
