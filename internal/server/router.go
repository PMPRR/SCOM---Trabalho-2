package server

import (
	"log/slog"
	"net/http"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(log *slog.Logger, db *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			log.Error("health check failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	static := http.FileServer(http.Dir("web/static"))
	mux.Handle("GET /static/", http.StripPrefix("/static", static))

	return middleware.Recover(log)(middleware.Logging(log)(mux))
}
