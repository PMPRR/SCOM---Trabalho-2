// Command ingest collects readings from the external sources once and exits.
// It is the manual counterpart of the collection the server runs on a timer:
// useful to fill the database while developing, or from cron.
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
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

func main() {
	_ = godotenv.Load()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("ingest stopped", "error", err)
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

	client := &http.Client{Timeout: 60 * time.Second}
	svc := service.NewIngestService(
		ingest.Sources(client, cfg.IngestCredentials()),
		repository.NewReadingRepository(pool),
		log,
	)

	failed := 0
	results := svc.RunOnce(ctx)
	for _, result := range results {
		if result.Err != nil {
			failed++
		}
	}
	if failed == len(results) {
		return fmt.Errorf("all %d sources failed", failed)
	}
	return nil
}
