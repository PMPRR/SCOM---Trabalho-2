-include .env
export

COMPOSE ?= podman compose

.PHONY: db run test ingest seed-dev promote-admin migrate-up migrate-down migrate-status schema

db:
	$(COMPOSE) up -d db

run:
	go run ./cmd/server

test:
	go test ./...

# Coleta as fontes externas uma vez (Open-Meteo; CEMADEN se houver
# credenciais no .env).
ingest:
	go run ./cmd/ingest

# Estacoes simuladas para desenvolvimento. Nao e migracao.
seed-dev:
	@$(COMPOSE) exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		-v ON_ERROR_STOP=1 < db/seed_dev.sql

# Promove uma conta ja cadastrada a administrador: make promote-admin EMAIL=ana@example.com
# Nenhum formulario do site cria administradores; o primeiro nasce aqui. O
# e-mail entra como variavel do psql (:'email'), que o cita com seguranca,
# e nao colado no texto do SQL.
promote-admin:
	@test -n "$(EMAIL)" || { echo "uso: make promote-admin EMAIL=ana@example.com"; exit 1; }
	@echo "UPDATE users SET role = 'admin' WHERE email = lower(:'email') RETURNING email, role;" \
		| $(COMPOSE) exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
			-v ON_ERROR_STOP=1 -v email="$(EMAIL)"

migrate-up:
	@goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	@goose -dir migrations postgres "$(DATABASE_URL)" status

schema:
	@$(COMPOSE) exec -T db pg_dump -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		--schema-only --no-owner -T goose_db_version > db/schema.sql
