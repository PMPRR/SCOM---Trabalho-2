package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// SessionCookie is the name of the cookie that carries the session token.
const SessionCookie = "climasp_session"

type contextKey int

const (
	userKey contextKey = iota
	csrfKey
)

// Authenticator recognizes the user behind a session token.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (domain.User, error)
}

// LoadUser reads the session cookie and, when it names a valid session,
// makes the user available to the rest of the request. It never refuses a
// request: pages that are open to everyone still want to know who is
// looking, and the refusing is left to RequireAuth and RequireRole.
func LoadUser(auth Authenticator, rd *render.Renderer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookie)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			user, err := auth.Authenticate(r.Context(), cookie.Value)
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), userKey, user))
			case errors.Is(err, domain.ErrUnauthenticated):
				// Expired or revoked session: the visitor is anonymous.
			default:
				rd.Fail(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserFrom returns the logged-in user of the request, if there is one.
func UserFrom(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userKey).(domain.User)
	return user, ok
}

// RequireAuth lets only logged-in users through. Someone browsing is sent
// to the login page; a request made by HTMX or by an API client gets 401,
// with the HX-Redirect header so HTMX takes the whole page to the login.
func RequireAuth(rd *render.Renderer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserFrom(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/entrar")
				rd.Fail(w, r, domain.ErrUnauthenticated)
				return
			}
			if r.Method == http.MethodGet && acceptsHTML(r) {
				http.Redirect(w, r, "/entrar", http.StatusSeeOther)
				return
			}
			rd.Fail(w, r, domain.ErrUnauthenticated)
		})
	}
}

// RequireRole lets through only users with the given role. It answers 403
// to a logged-in user with another role, and behaves like RequireAuth for
// someone who is not logged in.
func RequireRole(role domain.Role, rd *render.Renderer) func(http.Handler) http.Handler {
	requireAuth := RequireAuth(rd)
	return func(next http.Handler) http.Handler {
		return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user, _ := UserFrom(r.Context()); user.Role != role {
				rd.Fail(w, r, domain.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func acceptsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return accept == "" || containsToken(accept, "text/html") || containsToken(accept, "*/*")
}
