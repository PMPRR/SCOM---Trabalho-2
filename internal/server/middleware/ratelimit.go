package middleware

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// Limiter decides whether one more event is allowed for a key.
type Limiter interface {
	Allow(key string) (bool, time.Duration)
}

// RateLimit caps how often one address may call a route. It guards the
// login and sign-up forms against password guessing and mass registration.
//
// The address is the one of the TCP connection. Headers such as
// X-Forwarded-For are ignored on purpose: the client writes them, so
// trusting them would let an attacker pick a new identity per request.
func RateLimit(limiter Limiter, rd *render.Renderer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, wait := limiter.Allow(clientIP(r))
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
				rd.Fail(w, r, domain.ErrTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
