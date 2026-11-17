package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// accountPath is where a user lands after logging in or signing up.
const accountPath = "/conta"

// AuthService is what the handlers need from the business layer.
type AuthService interface {
	Register(ctx context.Context, in service.RegisterInput) (domain.User, error)
	Login(ctx context.Context, email, password string, remember bool) (service.Session, error)
	OpenSession(ctx context.Context, user domain.User, remember bool) (service.Session, error)
	Logout(ctx context.Context, token string) error
	ListUsers(ctx context.Context, actor domain.User) ([]domain.User, error)
}

// AuthHandler serves sign-up, login, logout and the pages behind them.
type AuthHandler struct {
	auth   AuthService
	render *render.Renderer
	// secureCookies marks the cookies Secure (HTTPS only). It is off in
	// development, where the site runs over plain HTTP on localhost.
	secureCookies bool
}

func NewAuthHandler(auth AuthService, rd *render.Renderer, secureCookies bool) *AuthHandler {
	return &AuthHandler{auth: auth, render: rd, secureCookies: secureCookies}
}

// LoginPage handles GET /entrar
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.redirectIfLoggedIn(w, r) {
		return
	}
	h.render.HTML(w, r, http.StatusOK, "login", newPageData(r, ""))
}

// SignupPage handles GET /cadastro
func (h *AuthHandler) SignupPage(w http.ResponseWriter, r *http.Request) {
	if h.redirectIfLoggedIn(w, r) {
		return
	}
	h.render.HTML(w, r, http.StatusOK, "signup", newPageData(r, ""))
}

// Login handles POST /entrar
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	session, err := h.auth.Login(r.Context(), email, r.PostFormValue("senha"), r.PostFormValue("lembrar") != "")
	if err != nil {
		h.formFailed(w, r, "login", err, map[string]string{"email": email})
		return
	}
	h.setSessionCookie(w, session)
	h.redirect(w, r, accountPath)
}

// Signup handles POST /cadastro. A new account is logged in right away.
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	in := service.RegisterInput{
		Name:            r.PostFormValue("nome"),
		Email:           r.PostFormValue("email"),
		Password:        r.PostFormValue("senha"),
		PasswordConfirm: r.PostFormValue("senha-confirma"),
		AcceptedTerms:   r.PostFormValue("termos") != "",
	}
	user, err := h.auth.Register(r.Context(), in)
	if err != nil {
		h.formFailed(w, r, "signup", err, map[string]string{"nome": in.Name, "email": in.Email})
		return
	}
	session, err := h.auth.OpenSession(r.Context(), user, false)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.setSessionCookie(w, session)
	h.redirect(w, r, accountPath)
}

// Logout handles POST /sair. It is a POST, protected by the CSRF check, so
// that a link on another site cannot log the user out.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(middleware.SessionCookie); err == nil {
		if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
			h.render.Fail(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: middleware.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	h.redirect(w, r, "/")
}

// Account handles GET /conta. The route is behind RequireAuth.
func (h *AuthHandler) Account(w http.ResponseWriter, r *http.Request) {
	data := newPageData(r, "")
	data.Data = newAccountView(*data.User)
	h.render.HTML(w, r, http.StatusOK, "account", data)
}

// Users handles GET /admin/usuarios. The route is behind RequireRole(admin).
func (h *AuthHandler) Users(w http.ResponseWriter, r *http.Request) {
	data := newPageData(r, "")
	users, err := h.auth.ListUsers(r.Context(), *data.User)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	views := make([]accountView, 0, len(users))
	for _, user := range users {
		views = append(views, newAccountView(user))
	}
	data.Data = views
	h.render.HTML(w, r, http.StatusOK, "users", data)
}

// formFailed answers a refused form. HTMX gets the error as a fragment, put
// in the form's notice area; a plain form post gets the whole page again,
// with the message and what had been typed. The status code is the same.
func (h *AuthHandler) formFailed(w http.ResponseWriter, r *http.Request, page string, err error, typed map[string]string) {
	if r.Header.Get("HX-Request") == "true" {
		h.render.Fail(w, r, err)
		return
	}
	status, _, message, _ := h.render.Describe(err)
	if status == http.StatusInternalServerError {
		h.render.Fail(w, r, err)
		return
	}
	data := newPageData(r, "")
	data.FormError = message
	data.Form = typed
	h.render.HTML(w, r, status, page, data)
}

// redirect sends the browser to another page after a successful form. HTMX
// follows the HX-Redirect header with a full page load; a plain form post
// gets 303, which makes the browser fetch the target with GET.
func (h *AuthHandler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *AuthHandler) redirectIfLoggedIn(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := middleware.UserFrom(r.Context()); !ok {
		return false
	}
	http.Redirect(w, r, accountPath, http.StatusSeeOther)
	return true
}

// setSessionCookie hands the session token to the browser. HttpOnly keeps
// it out of reach of scripts (an XSS cannot steal the session), SameSite=Lax
// keeps other sites from sending it with their own requests, and Secure
// restricts it to HTTPS outside development.
func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, session service.Session) {
	cookie := &http.Cookie{
		Name: middleware.SessionCookie, Value: session.Token, Path: "/",
		HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	}
	// Without Expires the cookie ends with the browser window; the server
	// side of the session expires on its own either way.
	if session.Persistent {
		cookie.Expires = session.ExpiresAt
		cookie.MaxAge = int(time.Until(session.ExpiresAt).Seconds())
	}
	http.SetCookie(w, cookie)
}

// accountView is a user as the account pages print it.
type accountView struct {
	Name    string
	Email   string
	Role    string
	IsAdmin bool
	Since   string
}

var roleLabels = map[domain.Role]string{
	domain.RoleOperator: "Operador",
	domain.RoleAdmin:    "Administrador",
}

func newAccountView(user domain.User) accountView {
	return accountView{
		Name:    user.Name,
		Email:   user.Email,
		Role:    roleLabels[user.Role],
		IsAdmin: user.IsAdmin(),
		Since:   user.CreatedAt.In(saoPaulo).Format("02/01/2006"),
	}
}
