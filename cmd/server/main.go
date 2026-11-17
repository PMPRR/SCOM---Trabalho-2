package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/config"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/ingest"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/repository"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

func main() {
	_ = godotenv.Load()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create db pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to db: %w", err)
	}

	router, err := server.NewRouter(log, pool, cfg.Env == "dev")
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Collection of external readings, in the background. It stops with ctx,
	// together with the server.
	if cfg.IngestInterval > 0 {
		collector := service.NewIngestService(
			ingest.Sources(&http.Client{Timeout: 60 * time.Second}, cfg.IngestCredentials()),
			repository.NewReadingRepository(pool),
			log,
		)
		go collector.Run(ctx, cfg.IngestInterval)
		log.Info("ingest scheduled", "interval", cfg.IngestInterval)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("server listening", "addr", cfg.Addr, "env", cfg.Env)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
