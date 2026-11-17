package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

const (
	// CSRFCookie carries the CSRF token of this browser.
	CSRFCookie = "climasp_csrf"
	// CSRFHeader is where HTMX sends the token (hx-headers on <body>).
	CSRFHeader = "X-CSRF-Token"
	// CSRFField is the hidden field that carries it in plain form posts.
	CSRFField = "csrf_token"

	maxFormBytes = 1 << 20 // no form of this site comes close to 1 MiB
)

// CSRF protects every request that changes something (anything other than
// GET, HEAD and OPTIONS) with the double-submit pattern.
//
// Each browser gets a random token in a cookie, and every page embeds the
// same token: in a hidden field of its forms and in the header HTMX adds to
// its requests. A state-changing request must bring the token both ways and
// the two must match. Another site can make the browser send the cookie,
// but it can neither read the cookie nor the page, so it cannot repeat the
// token in the header or in the field, and its forged request is refused.
//
// The cookie is HttpOnly because scripts never need it (the server writes
// the token into the page) and SameSite=Lax as a second line of defence.
func CSRF(rd *render.Renderer, secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			if cookie, err := r.Cookie(CSRFCookie); err == nil {
				token = cookie.Value
			}

			if !isSafeMethod(r.Method) {
				r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
				sent := r.Header.Get(CSRFHeader)
				if sent == "" {
					sent = r.PostFormValue(CSRFField)
				}
				if !security.EqualTokens(token, sent) {
					rd.Reject(w, r, http.StatusForbidden, "csrf_failed",
						"Não foi possível confirmar a origem do pedido. Recarregue a página e tente de novo.")
					return
				}
			}

			if token == "" {
				fresh, err := security.NewToken()
				if err != nil {
					rd.Fail(w, r, err)
					return
				}
				token = fresh
				http.SetCookie(w, &http.Cookie{
					Name: CSRFCookie, Value: token, Path: "/",
					HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
				})
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey, token)))
		})
	}
}

// CSRFToken returns the token the page must embed in its forms.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfKey).(string)
	return token
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// containsToken reports whether a comma-separated header lists value,
// ignoring parameters such as ";q=0.9".
func containsToken(header, value string) bool {
	for _, part := range strings.Split(header, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), value) {
			return true
		}
	}
	return false
}
