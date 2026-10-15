package config

import (
	"errors"
	"os"
)

type Config struct {
	Env         string
	Addr        string
	DatabaseURL string
}

func Load() (Config, error) {
	cfg := Config{
		Env:         getenv("APP_ENV", "key"),
		Addr:        getenv("APP_ADDR", ":8080"),
		DatabaseURL: getenv("DATABASE_URL", ""),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

// ---- Helper
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
