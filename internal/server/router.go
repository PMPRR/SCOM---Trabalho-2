package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/repository"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/handler"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// The server reads the site from disk, relative to the directory it is
// started in (the root of the repository, as `make run` does).
const (
	templatesDir = "web/templates"
	staticDir    = "web/static"
)

// NewRouter wires the layers together (repository -> service -> handler)
// and registers the routes. dev exposes technical error details.
func NewRouter(log *slog.Logger, db *pgxpool.Pool, dev bool) (http.Handler, error) {
	rd := render.New(log, dev)
	if err := rd.LoadTemplates(os.DirFS(templatesDir)); err != nil {
		return nil, fmt.Errorf("load templates from %s: %w", templatesDir, err)
	}

	rd.UseErrorPage(handler.ErrorPageData)

	authService, err := service.NewAuthService(
		repository.NewUserRepository(db), repository.NewSessionRepository(db),
		security.NewBcrypt(), log,
	)
	if err != nil {
		return nil, fmt.Errorf("start authentication: %w", err)
	}

	// Cookies are restricted to HTTPS everywhere except in development,
	// where the site runs over plain HTTP on localhost.
	secureCookies := !dev

	pages := handler.NewPageHandler(rd)
	auth := handler.NewAuthHandler(authService, rd, secureCookies)
	stations := handler.NewStationHandler(
		service.NewStationService(repository.NewStationRepository(db)), rd,
	)

	// Access rules, applied per route.
	loggedIn := middleware.RequireAuth(rd)
	adminOnly := middleware.RequireRole(domain.RoleAdmin, rd)
	// Ten attempts a minute per address: far above what a person needs to
	// log in, far below what guessing passwords needs.
	throttled := middleware.RateLimit(security.NewRateLimiter(10, time.Minute), rd)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			log.Error("health check failed", "error", err)
			rd.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		rd.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Pages (HTML). "/{$}" matches only the root, so unknown paths are 404
	// instead of falling back to the home page.
	mux.HandleFunc("GET /{$}", pages.Home)
	mux.HandleFunc("GET /mapa", pages.Map)

	// Authentication
	mux.HandleFunc("GET /entrar", auth.LoginPage)
	mux.HandleFunc("GET /cadastro", auth.SignupPage)
	mux.Handle("POST /entrar", throttled(http.HandlerFunc(auth.Login)))
	mux.Handle("POST /cadastro", throttled(http.HandlerFunc(auth.Signup)))
	mux.HandleFunc("POST /sair", auth.Logout)

	// Protected pages
	mux.Handle("GET /conta", loggedIn(http.HandlerFunc(auth.Account)))
	mux.Handle("GET /admin/usuarios", adminOnly(http.HandlerFunc(auth.Users)))

	// Fragments (HTML swapped into a page by HTMX)
	mux.HandleFunc("GET /estacoes", stations.Cards)
	mux.HandleFunc("GET /estacoes/{id}", stations.Card)

	// API (JSON)
	mux.HandleFunc("GET /api/stations", stations.List)
	mux.HandleFunc("GET /api/stations/{id}", stations.Get)

	static := http.FileServer(http.Dir(staticDir))
	mux.Handle("GET /static/", http.StripPrefix("/static", static))

	// From the outside in: recover from panics, log the request, check the
	// CSRF token, find out who is asking, then route.
	var root http.Handler = mux
	root = middleware.LoadUser(authService, rd)(root)
	root = middleware.CSRF(rd, secureCookies)(root)
	root = middleware.Logging(log)(root)
	root = middleware.Recover(log)(root)
	return root, nil
}
