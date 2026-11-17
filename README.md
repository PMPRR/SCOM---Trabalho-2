# ClimaSP — SCOM, Trabalho Individual II

Agregador de dados meteorológicos do Estado de São Paulo, continuação do
[Trabalho I](https://github.com/PMPRR/Projeto-Individual-SCOM). O backend
coleta medições de vários institutos, guarda em PostgreSQL/PostGIS e serve o
site com o mapa interativo e a API que o alimenta.

## Tecnologias

| Camada | Tecnologia |
|---|---|
| Servidor | Go 1.26 (`net/http`, `html/template`, `log/slog`) |
| Banco de dados | PostgreSQL 17 + PostGIS 3.5 |
| Acesso a dados | [pgx v5](https://github.com/jackc/pgx) |
| Senhas | bcrypt ([golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto/bcrypt)) |
| Migrações | [goose](https://github.com/pressly/goose) |
| Interface | HTML renderizado no servidor, HTMX 2.0 e Leaflet 1.9.4 |
| Contêiner do banco | Podman ou Docker Compose |

## Arquitetura

Arquitetura em camadas, com dependência em uma direção só:

```
handler ──► service ──► repository ──► domain
                │
                └────► ingest (fontes externas)
```

| Camada | Pasta | Responsabilidade |
|---|---|---|
| Interface | `internal/server`, `web/` | Rotas, páginas HTML, JSON e erros padronizados |
| Lógica de aplicação | `internal/service` | Regras de negócio e validação |
| Acesso a dados | `internal/repository` | Todo o SQL do sistema |
| Fontes externas | `internal/ingest` | Leitura das APIs dos institutos |
| Segurança | `internal/security` | Hash de senha, tokens e limite de tentativas |
| Domínio | `internal/domain` | Entidades, sem SQL nem HTTP |

## Estrutura do projeto

```
.
├── cmd/
│   ├── server/main.go            Servidor HTTP
│   └── ingest/main.go            Coleta manual das fontes externas
├── internal/
│   ├── config/                   Configuração lida do ambiente
│   ├── domain/                   Estação, observação, leitura, usuário e erros
│   ├── repository/               Consultas e gravações (pgx)
│   ├── service/                  Regras de estações, de coleta e de autenticação
│   ├── ingest/                   Open-Meteo e CEMADEN
│   ├── security/                 bcrypt, tokens aleatórios e limite de tentativas
│   └── server/
│       ├── router.go             Rotas e montagem das camadas
│       ├── handler/              Páginas, autenticação e API de estações
│       ├── render/               Templates, JSON e tratamento de erros
│       └── middleware/           Log, recuperação de panic, sessão, perfil, CSRF e limite
├── migrations/                   Migrações goose (SQL)
├── db/
│   ├── schema.sql                Script de criação, gerado por `make schema`
│   └── seed_dev.sql              Estações simuladas para desenvolvimento
├── web/
│   ├── templates/                layouts/, partials/ e pages/
│   └── static/                   css/, js/, img/, vendor/ e data/
├── docker-compose.yml            Serviço do banco (PostGIS)
├── .env.example                  Modelo de configuração
└── Makefile                      Atalhos de desenvolvimento
```

## Requisitos

| Programa | Versão | Necessário para |
|---|---|---|
| Go | ≥ 1.26 | compilar e executar |
| Podman ou Docker | com Compose | subir o banco |
| goose | v3 | aplicar as migrações |
| make | qualquer | atalhos do `Makefile` |

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Como executar

### 1. Clonar e configurar

```sh
git clone https://github.com/PMPRR/SCOM---Trabalho-2.git
cd SCOM---Trabalho-2
cp .env.example .env
```

Edite `.env` e troque `change-me` por uma senha sua, em `POSTGRES_PASSWORD` e
em `DATABASE_URL`. O arquivo `.env` não é versionado.

| Variável | Descrição | Padrão |
|---|---|---|
| `APP_ENV` | `dev` mostra detalhes técnicos nos erros e recarrega os templates | `dev` |
| `APP_ADDR` | Endereço em que o servidor escuta | `:8080` |
| `DATABASE_URL` | URL de conexão com o PostgreSQL | obrigatória |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Credenciais do contêiner do banco | — |
| `INGEST_INTERVAL` | Intervalo da coleta automática (`1h`, `30m`; mínimo `10m`) | desligada |
| `CEMADEN_EMAIL`, `CEMADEN_PASSWORD` | Conta da PED do CEMADEN; o token é renovado sozinho | fonte desligada |
| `CEMADEN_TOKEN` | Alternativa: token gerado à mão, válido por cerca de 4 h | fonte desligada |

### 2. Banco de dados

```sh
make db            # sobe o PostGIS (use COMPOSE="docker compose" para Docker)
make migrate-up    # cria as tabelas e os dados iniciais
```

Alternativa sem goose: executar `db/schema.sql` diretamente no banco.

### 3. Dados do mapa

Dois arquivos de malha já estão versionados em `web/static/data/`:

| Arquivo | Conteúdo |
|---|---|
| `sp-geo.js` | Contorno do Estado (obrigatório para o mapa abrir) |
| `sp-municipios.js` | Divisas dos 645 municípios e o seletor de município |

O terceiro, `relevo-sp.js` (faixas altimétricas e curvas de nível), é opcional
e não está no repositório: é gerado pelo `tools/build-sp-geo.py` do Trabalho I.
Sem ele o mapa funciona e as duas caixas de relevo ficam desabilitadas.

### 4. Estações

Escolha uma das duas formas, ou as duas:

```sh
make ingest        # coleta as fontes externas uma vez
make seed-dev      # grava 28 estações simuladas (só para desenvolvimento)
```

### 5. Servidor

```sh
make run
```

Abra <http://localhost:8080/>. Com `INGEST_INTERVAL` definido, o servidor
repete a coleta sozinho.

### 6. Primeiro administrador

O cadastro do site cria sempre contas de operador. Para ter um administrador,
cadastre-se em <http://localhost:8080/cadastro> e promova a conta:

```sh
make promote-admin EMAIL=voce@example.com
```

## Rotas

| Método | Rota | Resposta |
|---|---|---|
| GET | `/` | Página inicial |
| GET | `/mapa` | Mapa interativo |
| GET | `/entrar` | Formulário de login |
| POST | `/entrar` | Autentica e abre a sessão |
| GET | `/cadastro` | Formulário de cadastro |
| POST | `/cadastro` | Cria a conta (perfil operador) e já entra |
| POST | `/sair` | Encerra a sessão |
| GET | `/conta` | Dados da conta (exige login) |
| GET | `/admin/usuarios` | Lista de usuários (exige perfil administrador) |
| GET | `/estacoes?offset=` | Lista de estações, em cartões (fragmento HTML para o HTMX) |
| GET | `/estacoes/{id}` | Cartão de uma estação (fragmento HTML para o HTMX) |
| GET | `/api/stations?limit=&offset=` | Estações com a leitura mais recente (JSON) |
| GET | `/api/stations/{id}` | Uma estação (JSON) |
| GET | `/healthz` | Estado do servidor e do banco (JSON) |
| GET | `/static/...` | Arquivos estáticos |

O site usa HTMX: o servidor devolve HTML pronto e o navegador só troca o
trecho da página. As rotas de fragmento respondem aos erros também em HTML
quando a requisição vem do HTMX (cabeçalho `HX-Request`), e em JSON nos demais
casos. O JSON de `/api/stations` existe para o mapa, que é desenhado no
navegador pelo Leaflet.

O mapa oferece três campos interpolados (temperatura, chuva da última hora e
confiança da medição), setas de vento, divisas municipais e a consulta por
município. O campo de chuva usa todas as estações com pluviômetro, inclusive
as que não medem temperatura. Clicar em um marcador, ou em qualquer ponto do
Estado, abre um balão com o cartão da estação mais próxima, carregado pelo
HTMX; passar o mouse sobre um marcador mostra uma dica com nome e temperatura.

## Autenticação e controle de acesso

Há três níveis de acesso:

| Recurso | Visitante | Operador | Administrador |
|---|---|---|---|
| Página inicial, mapa, estações e `/api/stations` | sim | sim | sim |
| Cadastro e login | sim | sim | sim |
| `/conta` e `/sair` | não (vai para `/entrar`) | sim | sim |
| `/admin/usuarios` | não (vai para `/entrar`) | não (403) | sim |

Como funciona:

- **Senhas**: guardadas só como hash bcrypt (custo 12); mínimo de 8 caracteres, máximo de
  72 bytes. O
  login responde a mesma mensagem para e-mail inexistente e senha errada, e
  gasta o mesmo tempo nos dois casos.
- **Sessão**: no login o servidor sorteia um token de 32 bytes, envia-o no
  cookie `climasp_session` (`HttpOnly`, `SameSite=Lax`, `Secure` fora de
  `APP_ENV=dev`) e grava no banco apenas o SHA-256 dele, na tabela `sessions`.
  Sair apaga a linha, e o cookie antigo deixa de valer. Dura 12 h, ou 30 dias
  com "manter conectado".
- **Autorização**: os middlewares `RequireAuth` e `RequireRole` protegem as
  rotas no servidor; esconder o link no menu é só conveniência.
- **CSRF**: todo POST precisa repetir o valor do cookie `climasp_csrf`, no
  cabeçalho `X-CSRF-Token` (o HTMX envia sozinho) ou no campo `csrf_token` do
  formulário. Sem isso a resposta é 403.
- **Força bruta**: `POST /entrar` e `POST /cadastro` aceitam 10 tentativas por
  minuto por endereço IP; acima disso, 429 com `Retry-After`.
- **Entradas**: validadas no serviço; o SQL usa sempre parâmetros e os
  templates escapam o que exibem.
- **Logs**: criação de conta, login e falhas de autenticação são registrados,
  sem senhas nem tokens.

## Fontes de dados

| Fonte | Credencial | O que fornece |
|---|---|---|
| Open-Meteo | nenhuma | Modelo numérico amostrado em 28 localidades |
| CEMADEN | conta da PED ou token | Chuva da última hora nos pluviômetros automáticos |

Toda leitura é validada antes de ser gravada: posição dentro do Estado, data
recente e valores fisicamente possíveis.

Créditos exigidos pelas fontes: Dados da Rede Observacional do CEMADEN/MCTIC;
Open-Meteo (CC BY 4.0). Eles aparecem na página do mapa.

## Comandos do Makefile

| Comando | O que faz |
|---|---|
| `make db` | Sobe o contêiner do banco |
| `make run` | Executa o servidor |
| `make test` | Executa os testes automatizados |
| `make ingest` | Coleta as fontes externas uma vez |
| `make seed-dev` | Grava as estações simuladas |
| `make promote-admin EMAIL=...` | Promove uma conta cadastrada a administrador |
| `make migrate-up` / `migrate-down` / `migrate-status` | Migrações |
| `make schema` | Regera `db/schema.sql` a partir do banco |

## Modelo de dados

| Tabela | Conteúdo |
|---|---|
| `institutes` | Institutos de origem dos dados |
| `stations` | Estações, com localização geográfica e dono opcional |
| `observations` | Medições por estação e instante |
| `users` | Usuários, com perfil `operator` ou `admin` |
| `sessions` | Sessões abertas: hash do token, usuário e validade |

## Roadmap

- [x] Esqueleto do servidor, configuração e middlewares (`v0.1-skeleton`)
- [x] Banco de dados e migrações (`v0.2-database`)
- [x] API de leitura de estações
- [x] Site servido pelo backend
- [x] Coleta das fontes externas
- [x] Lista de estações e balão da estação no mapa em HTMX
- [x] Campo de chuva e divisas municipais
- [x] Painel de camadas do mapa reorganizado
- [x] Autenticação e controle de acesso (`v0.6-auth`)
- [ ] Cadastro, alteração e exclusão de estações
- [ ] Documentação da API e da arquitetura

## Autor

Paulo de Melo Pereira Rodrigues Ramalho
