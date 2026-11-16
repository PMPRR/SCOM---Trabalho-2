-include .env

COMPOSE ?= podman compose

export
.PHONY: db run test migrate-up migrate-down migrate-status schema

db:
	$(COMPOSE) up -d db

run:
	go run ./cmd/server

test:
	go test ./...

migrate-up:
	@goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	@goose -dir migrations postgres "$(DATABASE_URL)" status

schema:
	@$(COMPOSE) exec scom_trab2_db_1 pg_dump -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		--schema-only --no-owner -T goose_db_version > db/schema.sql
