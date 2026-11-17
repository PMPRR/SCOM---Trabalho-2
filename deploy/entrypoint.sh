#!/bin/sh
# Partida do contêiner do ClimaSP:
#   1. espera o banco aceitar conexões;
#   2. aplica as migrações pendentes (goose up), a menos que MIGRATE_ON_START=false;
#   3. troca este processo pelo servidor, que passa a receber o SIGTERM do
#      podman/docker stop e encerra com calma.
# Com argumentos, roda o comando pedido no lugar do servidor, depois dos
# passos 1 e 2. Exemplo: podman compose run --rm app ingest
set -eu

: "${DATABASE_URL:?defina DATABASE_URL (o compose monta a partir do .env)}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/app/migrations}"
DB_WAIT_SECONDS="${DB_WAIT_SECONDS:-60}"

goose_cmd() {
	goose -dir "$MIGRATIONS_DIR" postgres "$DATABASE_URL" "$@"
}

echo "entrypoint: esperando o banco (até ${DB_WAIT_SECONDS} s)"
waited=0
until goose_cmd status >/dev/null 2>&1; do
	waited=$((waited + 2))
	if [ "$waited" -ge "$DB_WAIT_SECONDS" ]; then
		echo "entrypoint: o banco não respondeu em ${DB_WAIT_SECONDS} s; último erro:" >&2
		goose_cmd status >&2 || true
		exit 1
	fi
	sleep 2
done

if [ "${MIGRATE_ON_START:-true}" = "true" ]; then
	echo "entrypoint: aplicando migrações"
	goose_cmd up
fi

if [ "$#" -gt 0 ]; then
	exec "$@"
fi
exec server
