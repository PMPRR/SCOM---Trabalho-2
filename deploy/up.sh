#!/bin/sh
# make up: sobe o ClimaSP inteiro e só termina quando estiver pronto.
#   1. confere o .env;
#   2. compila a imagem do servidor e sobe banco e servidor (compose);
#   3. espera o servidor responder (o banco inicializa e as migrações rodam
#      na partida do contêiner);
#   4. espera a primeira coleta das estações e mostra o resultado.
# Rodado pelo Makefile, que já carrega o .env e escolhe COMPOSE.
set -eu

COMPOSE="${COMPOSE:-podman compose}"
APP_PORT="${APP_PORT:-8080}"
UP_WAIT="${UP_WAIT:-300}"

step() { printf '\n==> %s\n' "$*"; }

if [ ! -f .env ]; then
	echo "Falta o arquivo .env. Crie a partir do modelo e troque a senha:"
	echo "  cp .env.example .env"
	exit 1
fi
: "${POSTGRES_USER:?defina POSTGRES_USER no .env}"
: "${POSTGRES_DB:?defina POSTGRES_DB no .env}"

step "Compilando o servidor e subindo o banco e o servidor ($COMPOSE)"
$COMPOSE up -d --build

step "Esperando o servidor (banco inicializando e migrações)"
waited=0
until $COMPOSE exec -T app wget -q -O /dev/null http://127.0.0.1:8080/healthz >/dev/null 2>&1; do
	waited=$((waited + 3))
	if [ "$waited" -ge "$UP_WAIT" ]; then
		echo "O servidor não respondeu em $UP_WAIT s. Últimas linhas do log:"
		$COMPOSE logs --tail 40 app || true
		exit 1
	fi
	sleep 3
done
echo "Servidor respondendo."

# Fontes coletadas: Open-Meteo sempre; CEMADEN se houver credenciais.
sources=1
if [ -n "${CEMADEN_TOKEN:-}" ] || { [ -n "${CEMADEN_EMAIL:-}" ] && [ -n "${CEMADEN_PASSWORD:-}" ]; }; then
	sources=2
fi

if [ "${INGEST_INTERVAL:-1h}" = "0" ]; then
	step "Coleta automática desligada (INGEST_INTERVAL=0)"
else
	step "Coletando os dados das estações"
	waited=0
	while :; do
		done_count=$($COMPOSE logs app 2>&1 | grep -c 'msg="ingest \(finished\|failed\)"' || true)
		[ "$done_count" -ge "$sources" ] && break
		waited=$((waited + 3))
		if [ "$waited" -ge "$UP_WAIT" ]; then
			echo "A coleta ainda não terminou; ela continua em segundo plano (make logs)."
			break
		fi
		sleep 3
	done
	$COMPOSE logs app 2>&1 | grep 'msg="ingest \(finished\|failed\)"' | tail -n "$sources" | sed 's/.*msg=/  /'
fi

step "Estações no banco"
$COMPOSE exec -T db psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -P footer=off -c "
	SELECT i.name AS fonte,
	       count(DISTINCT s.id) AS estacoes,
	       to_char(max(o.observed_at) AT TIME ZONE 'America/Sao_Paulo', 'DD/MM HH24:MI') AS ultima_leitura
	FROM institutes i
	JOIN stations s ON s.institute_id = i.id AND s.active
	LEFT JOIN observations o ON o.station_id = s.id
	GROUP BY i.name
	ORDER BY i.name;" || echo "(não foi possível ler o resumo do banco; o site continua no ar)"

printf '\nClimaSP no ar: http://localhost:%s\n' "$APP_PORT"
echo "Log: make logs   Parar: make down   Apagar o banco: make reset"
