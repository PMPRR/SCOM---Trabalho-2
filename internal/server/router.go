package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/repository"
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

	pages := handler.NewPageHandler(rd)
	stations := handler.NewStationHandler(
		service.NewStationService(repository.NewStationRepository(db)), rd,
	)

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
	mux.HandleFunc("GET /entrar", pages.Login)
	mux.HandleFunc("GET /cadastro", pages.Signup)

	// Fragments (HTML swapped into a page by HTMX)
	mux.HandleFunc("GET /estacoes", stations.Cards)
	mux.HandleFunc("GET /estacoes/{id}", stations.Card)

	// API (JSON)
	mux.HandleFunc("GET /api/stations", stations.List)
	mux.HandleFunc("GET /api/stations/{id}", stations.Get)

	static := http.FileServer(http.Dir(staticDir))
	mux.Handle("GET /static/", http.StripPrefix("/static", static))

	return middleware.Recover(log)(middleware.Logging(log)(mux)), nil
}
