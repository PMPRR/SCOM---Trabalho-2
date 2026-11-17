# Imagem do servidor ClimaSP. Funciona com Podman e com Docker:
#   podman build -t climasp -f Containerfile .
#   docker build -t climasp -f Containerfile .
# O jeito normal de usar é pelo compose, que sobe o banco junto:
#   podman compose up -d --build

# ---------------------------------------------------------------- compilação
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
# GOTOOLCHAIN=auto: se a imagem tiver um Go mais antigo que o pedido no
# go.mod, o go baixa a versão certa sozinho.
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=auto

# goose, para aplicar as migrações na partida. Só com o driver do
# PostgreSQL: as tags tiram os outros bancos e deixam o binário menor.
RUN go install \
      -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' \
      -ldflags='-s -w' \
      github.com/pressly/goose/v3/cmd/goose@v3.30.1

# Dependências antes do código: mudar o código não baixa tudo de novo.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server \
 && go build -trimpath -ldflags='-s -w' -o /out/ingest ./cmd/ingest

# ---------------------------------------------------------------- execução
FROM docker.io/library/alpine:3.22

# ca-certificates: a coleta fala HTTPS com o CEMADEN e o Open-Meteo.
# tzdata: horários dos logs no fuso de São Paulo.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 climasp

WORKDIR /app
COPY --from=build /go/bin/goose /usr/local/bin/goose
COPY --from=build /out/server /out/ingest /usr/local/bin/
COPY deploy/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 0755 /usr/local/bin/entrypoint.sh
# O servidor lê web/templates e web/static a partir da pasta de trabalho.
COPY migrations ./migrations
COPY web ./web

# Nada roda como root.
USER climasp

ENV APP_ADDR=:8080 \
    TZ=America/Sao_Paulo
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
