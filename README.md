# SCOM — Trabalho Individual II

Backend do agregador de dados meteorológicos do estado de São Paulo, continuação
do [Trabalho I](https://github.com/PMPRR/Projeto-Individual-SCOM). Reúne
medições de múltiplos institutos em um banco PostgreSQL/PostGIS e as expõe por
uma API HTTP para o mapa interativo.

## Tecnologias

| Camada | Tecnologia |
|---|---|
| Servidor | Go 1.26 (`net/http`, `log/slog`) |
| Banco de dados | PostgreSQL 17 + PostGIS 3.5 |
| Acesso a dados | [pgx v5](https://github.com/jackc/pgx) |
| Migrações | [goose](https://github.com/pressly/goose) |
| Contêiner do banco | Podman ou Docker Compose |

## Estrutura do projeto

```
.
├── cmd/server/main.go            Ponto de entrada e desligamento gracioso
├── internal/
│   ├── config/                   Configuração lida do ambiente
│   └── server/
│       ├── router.go             Rotas HTTP
│       └── middleware/           Log de requisições e recuperação de panic
├── migrations/                   Migrações goose (SQL)
├── db/schema.sql                 Script de criação do banco, gerado por `make schema`
├── docker-compose.yml            Serviço do banco (PostGIS)
├── .env.example                  Modelo de configuração
└── Makefile                      Atalhos de desenvolvimento
```

## Requisitos

| Programa | Versão | Necessário para |
|---|---|---|
| Go | ≥ 1.26 | compilar e executar o servidor |
| Podman ou Docker | com Compose | subir o banco |
| goose | v3 | aplicar as migrações |
| make | qualquer | atalhos do `Makefile` |

Instalação do goose:

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Como executar

### 1. Clonar o repositório

```sh
git clone https://github.com/PMPRR/SCOM---Trabalho-2.git
cd SCOM---Trabalho-2
```

### 2. Configurar o ambiente

```sh
cp .env.example .env
```

Edite `.env` e troque `change-me` por uma senha sua, tanto em
`POSTGRES_PASSWORD` quanto em `DATABASE_URL`. O arquivo `.env` não é versionado.

| Variável | Descrição | Padrão |
|---|---|---|
| `APP_ENV` | Ambiente (`dev` ou `prod`) | `dev` |
| `APP_ADDR` | Endereço em que o servidor escuta | `:8080` |
| `DATABASE_URL` | URL de conexão com o PostgreSQL | obrigatória |
| `POSTGRES_USER` | Usuário criado no contêiner do banco | — |
| `POSTGRES_PASSWORD` | Senha do usuário do banco | — |
| `POSTGRES_DB` | Nome do banco criado no contêiner | — |

### 3. Subir o banco e aplicar as migrações

```sh
make db            # sobe o PostGIS (use COMPOSE="docker compose" para Docker)
make migrate-up    # cria as tabelas e os dados iniciais
```

Alternativa sem goose: executar `db/schema.sql` diretamente no banco.

### 4. Iniciar o servidor

```sh
make run
```

Verificação:

```sh
curl http://localhost:8080/healthz
# {"status":"ok"}
```

## Comandos do Makefile

| Comando | O que faz |
|---|---|
| `make db` | Sobe o contêiner do banco |
| `make run` | Executa o servidor |
| `make test` | Executa os testes automatizados |
| `make migrate-up` | Aplica as migrações pendentes |
| `make migrate-down` | Desfaz a última migração |
| `make migrate-status` | Mostra o estado das migrações |
| `make schema` | Regera `db/schema.sql` a partir do banco |

## Modelo de dados

| Tabela | Conteúdo |
|---|---|
| `institutes` | Institutos de origem dos dados (INMET, Open-Meteo) |
| `stations` | Estações meteorológicas, com localização geográfica |
| `observations` | Medições por estação e instante |
| `users` | Usuários, com perfil `operator` ou `admin` |

## Roadmap

- [x] Esqueleto do servidor, configuração e middlewares (`v0.1-skeleton`)
- [x] Banco de dados e migrações (`v0.2-database`)
- [ ] API de estações e observações
- [ ] Autenticação e controle de acesso
- [ ] Integração com o frontend do Trabalho I
- [ ] Testes automatizados
- [ ] Documentação da API e da arquitetura

## Autor

Paulo de Melo Pereira Rodrigues Ramalho
