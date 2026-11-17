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
├── tools/
│   └── build-relevo.py           Gera relevo-sp.js a partir do SRTM30
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

Os três arquivos de dados do mapa já estão versionados em `web/static/data/`:

| Arquivo | Conteúdo | Fonte |
|---|---|---|
| `sp-geo.js` | Contorno do Estado (obrigatório para o mapa abrir) | IBGE |
| `sp-municipios.js` | Divisas dos 645 municípios e o seletor de município | IBGE |
| `relevo-sp.js` | Faixas altimétricas e curvas de nível, de 200 em 200 m | SRTM30 (NASA/USGS) |

O relevo é gerado por `tools/build-relevo.py` a partir do SRTM30 v2.1
(30 segundos de arco, cerca de 900 m), recortado pelo mesmo contorno de
`sp-geo.js`. Para gerar de novo:

```sh
pip install numpy contourpy shapely
python3 tools/build-relevo.py
```

Cada célula do SRTM30 é a média de quase 1 km², então os picos aparecem
abaixo da altitude do cume: a Pedra da Mina (2798 m) sai com cerca de 2570 m.
Sem o arquivo, o mapa funciona e as duas caixas de relevo ficam desabilitadas.

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
| GET | `/painel/estacoes?q=&offset=` | Painel de estações: as suas, ou todas para o administrador (exige login) |
| GET | `/painel/estacoes/linhas?q=&offset=` | Linhas da tabela do painel (fragmento HTML para o HTMX) |
| GET | `/painel/estacoes/nova` | Formulário de nova estação |
| POST | `/painel/estacoes` | Cadastra a estação do formulário |
| GET | `/painel/estacoes/{id}/editar` | Formulário de edição |
| PUT, POST | `/painel/estacoes/{id}` | Salva a edição (PUT pelo HTMX, POST sem JavaScript) |
| POST | `/painel/estacoes/{id}/ativa` | Ativa ou desativa (`active=true` ou `false`) |
| DELETE, POST | `/painel/estacoes/{id}` e `/{id}/excluir` | Exclui (DELETE pelo HTMX, POST sem JavaScript) |
| GET | `/estacoes?offset=` | Lista de estações, em cartões (fragmento HTML para o HTMX) |
| GET | `/estacoes/{id}` | Cartão de uma estação (fragmento HTML para o HTMX) |
| GET | `/api/stations?limit=&offset=` | Estações com a leitura mais recente (JSON) |
| GET | `/api/stations/{id}` | Uma estação ativa (JSON) |
| POST | `/api/stations` | Cadastra uma estação: 201 com `Location` (exige login) |
| PUT | `/api/stations/{id}` | Substitui os dados de uma estação: 200 (exige login) |
| PATCH | `/api/stations/{id}` | Ativa ou desativa, corpo `{"active": false}`: 200 (exige login) |
| DELETE | `/api/stations/{id}` | Exclui a estação e as leituras dela: 204 (exige login) |
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
| Painel de estações | não (vai para `/entrar`) | as suas | todas |
| Cadastrar estação | não (401) | sim, fica dono dela | sim, fica dono dela |
| Editar ou excluir estação | não (401) | só as suas (403 nas outras) | as cadastradas no site |
| Ativar ou desativar estação | não (401) | só as suas (403 nas outras) | todas, inclusive as oficiais |
| Editar ou excluir estação oficial | não (401) | não (403) | não (409) |

Como funciona:

- **Senhas**: guardadas só como hash bcrypt (custo 12); mínimo de 8
  caracteres, máximo de 72 bytes. O login responde a mesma mensagem para
  e-mail inexistente e senha errada, e gasta o mesmo tempo nos dois casos.
- **Sessão**: no login o servidor sorteia um token de 32 bytes, envia-o no
  cookie `climasp_session` (`HttpOnly`, `SameSite=Lax`, `Secure` fora de
  `APP_ENV=dev`) e grava no banco apenas o SHA-256 dele, na tabela `sessions`.
  Sair apaga a linha, e o cookie antigo deixa de valer. Dura 12 h, ou 30 dias
  com "manter conectado".
- **Autorização**: os middlewares `RequireAuth` e `RequireRole` protegem as
  rotas no servidor; quem pode mexer em cada estação é decidido no serviço
  (`internal/service/station_manage.go`). Esconder um botão ou link na página
  é só conveniência.
- **CSRF**: todo pedido que altera algo (POST, PUT, PATCH, DELETE) precisa
  repetir o valor do cookie `climasp_csrf`, no cabeçalho `X-CSRF-Token` (o
  HTMX envia sozinho) ou no campo `csrf_token` do formulário. Sem isso a
  resposta é 403.
- **Força bruta**: `POST /entrar` e `POST /cadastro` aceitam 10 tentativas por
  minuto por endereço IP; acima disso, 429 com `Retry-After`.
- **Entradas**: validadas no serviço; o SQL usa sempre parâmetros e os
  templates escapam o que exibem.
- **Logs**: criação de conta, login, falhas de autenticação e toda alteração
  de estação (com o id de quem fez) são registrados, sem senhas nem tokens.

## Cadastro e gerenciamento de estações

A estação é a entidade principal do sistema. Há dois tipos:

- **Oficiais**: coletadas de um instituto (Open-Meteo, CEMADEN), sem dono.
  Nome, código e posição vêm da fonte e são regravados a cada coleta, então
  ninguém as edita nem exclui pelo site (409); o administrador pode
  desativá-las, o que as tira do mapa sem apagar as leituras.
- **Particulares**: cadastradas no site por um operador ou administrador, que
  fica dono delas. Pertencem ao instituto `particular` (migração 00011).

Uma estação desativada some do mapa, de `/estacoes` e de `/api/stations`, mas
continua no painel de quem a gerencia.

Validação no servidor, para o formulário e para a API:

| Campo | Regra |
|---|---|
| `name` | 1 a 120 caracteres, sem caracteres de controle |
| `external_code` | 1 a 50 caracteres: letras sem acento, números, `.`, `_` e `-`; único entre as estações particulares |
| `lat`, `lon` | dentro da faixa do Estado (latitude −25,5 a −19,5; longitude −53,5 a −44); o formulário aceita vírgula decimal |
| `altitude_m` | opcional, de −100 a 3000 m |
| `active` | opcional na API (padrão `true`) |

A API exige o corpo em JSON (`Content-Type: application/json`, senão 415) e
recusa campos desconhecidos (400). Um erro sempre vem no mesmo formato, com o
campo culpado quando há um:

```json
{"error": {"code": "invalid_parameter", "message": "A latitude deve estar entre -25,5 e -19,5, a faixa do Estado de São Paulo.", "field": "lat"}}
```

Exemplo com `curl`, depois de entrar pelo site (copie os cookies
`climasp_session` e `climasp_csrf` do navegador; o valor de `climasp_csrf` vai
também no cabeçalho):

```sh
curl -i http://localhost:8080/api/stations \
  -H "Content-Type: application/json" \
  -H "Cookie: climasp_session=SESSAO; climasp_csrf=TOKEN" \
  -H "X-CSRF-Token: TOKEN" \
  -d '{"external_code":"sitio-01","name":"Sítio","lat":-23.55,"lon":-46.63}'
```

O painel usa HTMX: a busca filtra enquanto se digita, ativar troca só a linha
da tabela e excluir pede confirmação e remove a linha. O formulário tem um
mapa que preenche latitude e longitude com um clique e mostra o município do
ponto. Sem JavaScript, tudo funciona com formulários comuns.

## Fontes de dados

| Fonte | Credencial | O que fornece |
|---|---|---|
| Open-Meteo | nenhuma | Modelo numérico amostrado em 28 localidades |
| CEMADEN | conta da PED ou token | Chuva da última hora nos pluviômetros automáticos |

Toda leitura é validada antes de ser gravada: posição dentro do Estado, data
recente e valores fisicamente possíveis.

Créditos exigidos pelas fontes: Dados da Rede Observacional do CEMADEN/MCTIC;
Open-Meteo (CC BY 4.0); malhas do IBGE; altimetria SRTM30 (NASA/USGS, domínio
público). Eles aparecem na página do mapa.

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
| `institutes` | Institutos de origem dos dados, e `particular` para as estações cadastradas no site |
| `stations` | Estações, com localização geográfica, situação (ativa) e dono opcional |
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
- [x] Cadastro, alteração e exclusão de estações (`v0.7-stations-crud`)
- [ ] Documentação da API e da arquitetura

## Autor

Paulo de Melo Pereira Rodrigues Ramalho
