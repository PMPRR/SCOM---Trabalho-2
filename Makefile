-include .env
export

# Podman se estiver instalado; senao Docker. Para forcar: make up COMPOSE="docker compose"
COMPOSE ?= $(shell if command -v podman >/dev/null 2>&1; then echo podman compose; else echo docker compose; fi)

.PHONY: up down logs reset db run test ingest seed-dev promote-admin migrate-up migrate-down migrate-status schema schema-load

# Tudo com um comando, depois de criar o .env (cp .env.example .env):
# compila o servidor, sobe banco e servidor em conteineres, aplica as
# migracoes, faz a primeira coleta das estacoes e so termina com o site no ar.
up:
	@sh deploy/up.sh

# Para os conteineres; o banco fica guardado no volume.
down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f app

# Para tudo e apaga o banco. O proximo make up comeca do zero.
reset:
	$(COMPOSE) down -v

# So o banco, para rodar o servidor na maquina com make run.
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

# Regera db/schema.sql, o script que cria o banco inteiro sem o goose:
#   - o esquema, so com os objetos do projeto (a imagem do PostGIS cria
#     sozinha os schemas tiger, tiger_data e topology e outras extensoes,
#     que o projeto nao usa);
#   - as linhas de institutes, sem as quais coleta e cadastro nao funcionam;
#   - as linhas de goose_db_version, para o goose ver o banco em dia.
# As linhas \restrict do pg_dump mais novo saem: um psql mais antigo nao as
# conhece. Grava num arquivo temporario e so troca no fim, para uma falha no
# meio nao deixar o script vazio.
schema:
	@{ echo "-- Gerado por make schema. Cria o banco do ClimaSP inteiro: psql -d BANCO -f db/schema.sql"; \
	  $(COMPOSE) exec -T db pg_dump -U $(POSTGRES_USER) -d $(POSTGRES_DB) --schema-only --no-owner \
		--extension=postgis -N tiger -N tiger_data -N topology && \
	  $(COMPOSE) exec -T db pg_dump -U $(POSTGRES_USER) -d $(POSTGRES_DB) --data-only --no-owner --inserts \
		-t public.institutes -t public.goose_db_version; } > db/schema.sql.tmp \
		&& sed '/^\\\(un\)\?restrict /d' db/schema.sql.tmp > db/schema.sql \
		&& rm -f db/schema.sql.tmp \
		|| { rm -f db/schema.sql.tmp; echo "make schema falhou; db/schema.sql nao mudou"; exit 1; }

# Cria o banco a partir de db/schema.sql, sem o goose. So num banco vazio:
# o script cria tudo e para no primeiro erro.
schema-load:
	@$(COMPOSE) exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -v ON_ERROR_STOP=1 -q -o /dev/null < db/schema.sql
