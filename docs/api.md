# API do ClimaSP

Referência de todos os endpoints do servidor. A arquitetura está em
[arquitetura.md](arquitetura.md).

Os exemplos de resposta foram gravados das respostas reais dos handlers, com
os middlewares do servidor, e só formatados para leitura.

- [Convenções](#convenções)
- [API JSON de estações](#api-json-de-estações)
- [Autenticação](#autenticação)
- [Páginas HTML](#páginas-html)
- [Fragmentos HTMX](#fragmentos-htmx)
- [Painel de estações](#painel-de-estações)
- [Envio pelas estações](#post-apistationsidreadings)
- [Outros](#outros)

## Convenções

**Endereço base:** `http://localhost:8080` (configurável por `APP_ADDR`).

### Formatos

| Tipo de rota | Pede | Responde |
|---|---|---|
| API (`/api/...`) | JSON (`Content-Type: application/json`) | JSON, UTF-8 |
| Formulários (`/entrar`, `/cadastro`, `/painel/...`) | `application/x-www-form-urlencoded` | Redirecionamento, página ou fragmento |
| Fragmentos (`/estacoes`, `/painel/estacoes/linhas`) | — | HTML parcial para o HTMX |

Datas em RFC 3339 (ISO 8601), com o fuso da máquina do servidor: num
servidor em São Paulo, `-03:00`. Unidades: °C, %, mm, m/s, graus e hPa.
Coordenadas em graus decimais (WGS 84), latitude e longitude negativas no
Brasil.

### Autenticação nas rotas que alteram dados

A API usa a mesma **sessão por cookie** do site, não um token de API. Toda
requisição que altera algo (POST, PUT, PATCH, DELETE) precisa:

1. do cookie `climasp_session`, recebido ao entrar ou se cadastrar;
2. do cookie `climasp_csrf`, recebido em qualquer página;
3. do **mesmo valor** de `climasp_csrf` no cabeçalho `X-CSRF-Token` (ou, num
   formulário, no campo `csrf_token`).

Roteiro completo com `curl` (o token aparece no campo oculto de qualquer
formulário do site):

```sh
B=http://localhost:8080

# 1. pega o token CSRF e o cookie dele
TOKEN=$(curl -s -c jar.txt -b jar.txt $B/entrar \
  | grep -o 'name="csrf_token" value="[^"]*"' | head -1 | sed 's/.*value="\([^"]*\)"/\1/')

# 2. entra (guarda o cookie de sessão)
curl -s -o /dev/null -c jar.txt -b jar.txt \
  --data-urlencode "csrf_token=$TOKEN" \
  -d "email=ana@example.com" -d "senha=senha-bem-longa" $B/entrar

# 3. usa a API
curl -i -b jar.txt -H "X-CSRF-Token: $TOKEN" -H "Content-Type: application/json" \
  -d '{"external_code":"sitio-01","name":"Sítio da Ana","lat":-22.95,"lon":-46.55}' \
  $B/api/stations
```

A verificação do CSRF vem antes da de login: um pedido sem cookie nenhum
recebe 403 `csrf_failed`; com o cookie CSRF mas sem sessão, 401
`unauthenticated`.

A única exceção é [`POST /api/stations/{id}/readings`](#post-apistationsidreadings),
usada pelas próprias estações: ela não usa cookie nem CSRF, e cada pedido é
autenticado pela assinatura Ed25519 do corpo.

### Formato de erro

Todo erro tem o mesmo formato:

```json
{
  "error": {
    "code": "invalid_parameter",
    "field": "lat",
    "message": "A latitude deve estar entre -25,5 e -19,5, a faixa do Estado de São Paulo.",
    "detail": "..."
  }
}
```

| Campo | Presença | Conteúdo |
|---|---|---|
| `code` | sempre | Código estável, para programas |
| `message` | sempre | Texto em português, para pessoas |
| `field` | quando o erro é de um campo | Nome do campo ou parâmetro culpado |
| `detail` | só com `APP_ENV=dev`, em erro 500 | Causa técnica |

| `code` | Status | Quando |
|---|---|---|
| `invalid_parameter` | 400 | Parâmetro ou campo inválido (veja `field`); `field: "body"` para JSON malformado ou com campo desconhecido |
| `unauthenticated` | 401 | Rota exige login |
| `invalid_credentials` | 401 | E-mail ou senha incorretos |
| `invalid_signature` | 401 | Leitura enviada por estação sem assinatura, ou com assinatura que não confere |
| `forbidden` | 403 | Usuário logado sem permissão |
| `csrf_failed` | 403 | Token CSRF ausente ou diferente do cookie |
| `uplink_disabled` | 403 | Leitura enviada a uma estação sem chave cadastrada |
| `not_found` | 404 | Recurso não existe (ou estação desativada, na leitura pública) |
| `email_taken` | 409 | E-mail já cadastrado |
| `station_code_taken` | 409 | Código de estação particular repetido |
| `official_station` | 409 | Tentativa de editar ou excluir uma estação oficial |
| `key_taken` | 409 | Chave pública já usada por outra estação |
| `payload_too_large` | 413 | Leitura enviada com mais de 16 KiB |
| `unsupported_media_type` | 415 | Corpo que não é JSON numa rota da API |
| `too_many_requests` | 429 | Mais de 10 tentativas por minuto em login ou cadastro, ou de 120 envios de leitura por minuto do mesmo IP (com `Retry-After`) |
| `too_soon` | 429 | Leitura enviada antes do fim do intervalo da estação (com `Retry-After`) |
| `internal_error` | 500 | Erro inesperado; a causa vai para o log |

O mesmo erro muda de **formato**, nunca de status, conforme quem pede:

| Quem pede | Resposta de erro |
|---|---|
| Cliente da API (`Accept` diferente de `text/html`, como o `curl`) | JSON acima |
| Navegador abrindo a URL (GET com `Accept: text/html`) | Página de erro do site |
| HTMX (cabeçalho `HX-Request: true`) | `<p class="request-error" role="alert" data-field="...">mensagem</p>` |

Erro interno com `APP_ENV=dev` e em produção:

```json
{"error": {"code": "internal_error", "message": "Erro interno do servidor.", "detail": "list stations: connection refused"}}
{"error": {"code": "internal_error", "message": "Erro interno do servidor."}}
```

### Paginação

As listagens usam `limit` e `offset` e devolvem o total:

```json
"pagination": {"limit": 50, "offset": 0, "total": 832}
```

A próxima página começa em `offset + limit`; acabou quando
`offset + limit >= total`.

## API JSON de estações

### Objeto estação

| Campo | Tipo | Descrição |
|---|---|---|
| `id` | inteiro | Identificador |
| `institute_code` | texto | Instituto de origem: `openmeteo`, `cemaden` ou `particular` (cadastrada no site) |
| `institute_name` | texto | Nome do instituto |
| `external_code` | texto | Código da estação no instituto, ou o escolhido pelo dono |
| `name` | texto | Nome |
| `lat`, `lon` | número | Posição, graus decimais |
| `altitude_m` | número ou `null` | Altitude em metros |
| `active` | booleano | `false` tira a estação do mapa e da leitura pública |
| `owner_id` | inteiro ou `null` | Usuário que cadastrou; `null` = estação oficial de um instituto |
| `created_at`, `updated_at` | data | Cadastro e última alteração |
| `latest` | objeto ou `null` | Leitura mais recente (abaixo); `null` sem leitura |
| `confidence` | texto | `high`, `medium`, `low`, ou `""` sem leitura |
| `public_key` | texto | Só em estações com envio automático: chave pública Ed25519, `ssh-ed25519 AAAA...` |
| `key_fingerprint` | texto | Só com chave: impressão digital SHA-256, como `ssh-keygen -l` mostra |
| `report_interval_s` | inteiro ou `null` | Intervalo de envio combinado, em segundos |
| `last_report_at` | data ou `null` | Último envio aceito da própria estação |
| `uplink_status` | texto | Só com chave: `waiting` (nenhum envio ainda), `ok`, `late` (dois intervalos sem envio) |

Objeto `latest` (um sensor que a estação não tem vem `null`):

| Campo | Unidade |
|---|---|
| `observed_at` | data da medição |
| `temperature_c` | °C |
| `humidity_pct` | % |
| `precipitation_mm` | mm na última hora |
| `wind_speed_ms` | m/s |
| `wind_dir_deg` | graus, de onde o vento vem |
| `pressure_hpa` | hPa |

Regra da `confidence`: leitura com 3 h ou mais, ou vinda de modelo numérico
(Open-Meteo) → `low`; estação oficial → `high`; estação particular →
`medium`.

---

### `GET /api/stations`

Lista as estações ativas, com a leitura mais recente de cada uma, em ordem de
`id`. Aberta a todos.

**Parâmetros de consulta**

| Nome | Tipo | Padrão | Regra |
|---|---|---|---|
| `limit` | inteiro | 50 | 1 a 500 |
| `offset` | inteiro | 0 | ≥ 0 |

**Exemplo de requisição**

```sh
curl "http://localhost:8080/api/stations?limit=2"
```

**Exemplo de resposta** — `200 OK`

```json
{
  "data": [
    {
      "id": 1,
      "institute_code": "openmeteo",
      "institute_name": "Open-Meteo",
      "external_code": "campinas",
      "name": "Campinas",
      "lat": -22.9056,
      "lon": -47.0608,
      "altitude_m": null,
      "active": true,
      "owner_id": null,
      "created_at": "2026-10-18T07:00:00-03:00",
      "updated_at": "2026-10-18T07:00:00-03:00",
      "latest": {
        "observed_at": "2026-11-17T07:00:00-03:00",
        "temperature_c": 24.3,
        "humidity_pct": 61,
        "precipitation_mm": null,
        "wind_speed_ms": 3.1,
        "wind_dir_deg": 140,
        "pressure_hpa": 1016.2
      },
      "confidence": "low",
      "report_interval_s": null,
      "last_report_at": null
    },
    {
      "id": 2,
      "institute_code": "cemaden",
      "institute_name": "CEMADEN",
      "external_code": "355030801A",
      "name": "São Paulo - Sé",
      "lat": -23.5505,
      "lon": -46.6333,
      "altitude_m": null,
      "active": true,
      "owner_id": null,
      "created_at": "2026-10-19T03:00:00-03:00",
      "updated_at": "2026-10-19T03:00:00-03:00",
      "latest": {
        "observed_at": "2026-11-17T07:40:00-03:00",
        "temperature_c": null,
        "humidity_pct": null,
        "precipitation_mm": 1.6,
        "wind_speed_ms": null,
        "wind_dir_deg": null,
        "pressure_hpa": null
      },
      "confidence": "high",
      "report_interval_s": null,
      "last_report_at": null
    }
  ],
  "pagination": {"limit": 2, "offset": 0, "total": 2}
}
```

**Erros** — `400 Bad Request`

```json
{"error": {"code": "invalid_parameter", "field": "limit", "message": "O parâmetro limit deve ser um número inteiro."}}
{"error": {"code": "invalid_parameter", "field": "limit", "message": "O limite deve estar entre 1 e 500."}}
{"error": {"code": "invalid_parameter", "field": "offset", "message": "O deslocamento não pode ser negativo."}}
```

| Status | Quando |
|---|---|
| 200 | Sucesso (lista vazia é `"data": []`) |
| 400 | `limit` ou `offset` inválido |
| 500 | Erro interno |

---

### `GET /api/stations/{id}`

Uma estação ativa. Aberta a todos.

**Parâmetros de caminho**

| Nome | Tipo | Regra |
|---|---|---|
| `id` | inteiro | ≥ 1 |

**Exemplo de requisição**

```sh
curl http://localhost:8080/api/stations/2
```

**Exemplo de resposta** — `200 OK`

```json
{
  "data": {
    "id": 2,
    "institute_code": "cemaden",
    "institute_name": "CEMADEN",
    "external_code": "355030801A",
    "name": "São Paulo - Sé",
    "lat": -23.5505,
    "lon": -46.6333,
    "altitude_m": null,
    "active": true,
    "owner_id": null,
    "created_at": "2026-10-19T03:00:00-03:00",
    "updated_at": "2026-10-19T03:00:00-03:00",
    "latest": {
      "observed_at": "2026-11-17T07:40:00-03:00",
      "temperature_c": null,
      "humidity_pct": null,
      "precipitation_mm": 1.6,
      "wind_speed_ms": null,
      "wind_dir_deg": null,
      "pressure_hpa": null
    },
    "confidence": "high",
    "report_interval_s": null,
    "last_report_at": null
  }
}
```

**Erros**

```json
{"error": {"code": "invalid_parameter", "field": "id", "message": "O id da estação deve ser um número inteiro."}}
{"error": {"code": "invalid_parameter", "field": "id", "message": "O id da estação deve ser um inteiro positivo."}}
{"error": {"code": "not_found", "message": "Recurso não encontrado."}}
```

| Status | Quando |
|---|---|
| 200 | Sucesso |
| 400 | `id` não é inteiro, ou é menor que 1 |
| 404 | Não existe, ou está desativada |
| 500 | Erro interno |

---

### `POST /api/stations`

Cadastra uma estação particular. Quem cadastra fica dono dela.
**Exige login** (operador ou administrador) e token CSRF.

**Corpo** (`Content-Type: application/json`; campo desconhecido é recusado)

| Campo | Tipo | Obrigatório | Regra |
|---|---|---|---|
| `external_code` | texto | sim | 1 a 50 caracteres: `A-Z a-z 0-9 . _ -`, começando por letra ou número; único entre as estações particulares |
| `name` | texto | sim | 1 a 120 caracteres, sem caracteres de controle (espaços nas pontas são removidos) |
| `lat` | número | sim | −25,5 a −19,5 |
| `lon` | número | sim | −53,5 a −44 |
| `altitude_m` | número | não | −100 a 3000 |
| `active` | booleano | não | padrão `true` |
| `public_key` | texto | não | Linha do `.pub` de uma chave `ssh-keygen -t ed25519`; a estação passa a [enviar as próprias leituras](#post-apistationsidreadings). O comentário é descartado. Única entre as estações |
| `report_interval_s` | inteiro | com `public_key` | 60 a 86400 segundos; sem `public_key`, ignorado |

**Exemplo de requisição**

```sh
curl -i -b jar.txt -H "X-CSRF-Token: $TOKEN" -H "Content-Type: application/json" \
  -d '{"external_code":"sitio-01","name":"Sítio da Ana","lat":-22.95,"lon":-46.55,"altitude_m":760}' \
  http://localhost:8080/api/stations
```

**Exemplo de resposta** — `201 Created`, com `Location: /api/stations/3`

```json
{
  "data": {
    "id": 3,
    "institute_code": "particular",
    "institute_name": "Estação particular",
    "external_code": "sitio-01",
    "name": "Sítio da Ana",
    "lat": -22.95,
    "lon": -46.55,
    "altitude_m": 760,
    "active": true,
    "owner_id": 1,
    "created_at": "2026-11-17T07:49:14.481207-03:00",
    "updated_at": "2026-11-17T07:49:14.481207-03:00",
    "latest": null,
    "confidence": "",
    "report_interval_s": null,
    "last_report_at": null
  }
}
```

**Erros**

```json
{"error": {"code": "invalid_parameter", "field": "lat", "message": "A latitude deve estar entre -25,5 e -19,5, a faixa do Estado de São Paulo."}}
{"error": {"code": "invalid_parameter", "field": "body", "message": "O corpo da requisição não é um JSON válido para esta rota. Confira os nomes e os tipos dos campos."}}
{"error": {"code": "unsupported_media_type", "message": "Envie o corpo em JSON, com o cabeçalho Content-Type: application/json."}}
{"error": {"code": "station_code_taken", "field": "external_code", "message": "Já existe uma estação particular com este código."}}
{"error": {"code": "unauthenticated", "message": "Entre na sua conta para continuar."}}
{"error": {"code": "csrf_failed", "message": "Não foi possível confirmar a origem do pedido. Recarregue a página e tente de novo."}}
```

Mensagens de validação dos demais campos:

| `field` | Mensagem |
|---|---|
| `name` | "Informe o nome da estação." / "O nome pode ter no máximo 120 caracteres." / "O nome não pode ter caracteres de controle." |
| `external_code` | "O código deve ter de 1 a 50 caracteres: letras sem acento, números, ponto, hífen ou sublinhado." |
| `lat` | "Informe a latitude." / "A latitude deve estar entre -25,5 e -19,5, a faixa do Estado de São Paulo." |
| `lon` | "Informe a longitude." / "A longitude deve estar entre -53,5 e -44, a faixa do Estado de São Paulo." |
| `altitude_m` | "A altitude deve estar entre -100 e 3000 metros." |
| `public_key` | "A chave deve ser a linha do arquivo .pub de uma chave criada com ssh-keygen -t ed25519 (começa com ssh-ed25519)." |
| `report_interval_s` | "O intervalo de envio deve ficar entre 1 minuto e 24 horas." |

| Status | Quando |
|---|---|
| 201 | Criada; `Location` aponta para ela |
| 400 | Campo inválido (veja `field`) ou JSON malformado (`field: "body"`) |
| 401 | Sem sessão |
| 403 | Sem token CSRF ou token diferente |
| 409 | `external_code` já usado por outra estação particular (`station_code_taken`), ou chave já usada (`key_taken`) |
| 415 | Corpo que não é JSON |
| 500 | Erro interno |

---

### `PUT /api/stations/{id}`

Substitui todos os dados editáveis de uma estação particular. **Exige login**
e token CSRF. O operador só altera as suas; o administrador, qualquer
particular. Estações oficiais não são editáveis: os dados delas vêm da fonte e
seriam regravados na próxima coleta.

**Parâmetros de caminho:** `id` (inteiro ≥ 1).
**Corpo:** o mesmo de `POST /api/stations`; campo omitido volta ao padrão
(`altitude_m` → `null`, `active` → `true`, `public_key` → sem envio
automático). Uma chave nova invalida a anterior na hora e recomeça a contagem
do intervalo.

**Exemplo de requisição**

```sh
curl -X PUT -b jar.txt -H "X-CSRF-Token: $TOKEN" -H "Content-Type: application/json" \
  -d '{"external_code":"sitio-01","name":"Sítio da Ana (sede)","lat":-22.95,"lon":-46.55,"altitude_m":765,"active":true}' \
  http://localhost:8080/api/stations/3
```

**Exemplo de resposta** — `200 OK`

```json
{
  "data": {
    "id": 3,
    "institute_code": "particular",
    "institute_name": "Estação particular",
    "external_code": "sitio-01",
    "name": "Sítio da Ana (sede)",
    "lat": -22.95,
    "lon": -46.55,
    "altitude_m": 765,
    "active": true,
    "owner_id": 1,
    "created_at": "2026-11-17T07:49:14.481207-03:00",
    "updated_at": "2026-11-17T07:52:03.902114-03:00",
    "latest": null,
    "confidence": "",
    "report_interval_s": null,
    "last_report_at": null
  }
}
```

**Erros**

```json
{"error": {"code": "forbidden", "message": "Você não tem permissão para acessar isto."}}
{"error": {"code": "official_station", "message": "Esta estação é coletada de um instituto: os dados vêm da fonte. Ela só pode ser ativada ou desativada."}}
```

| Status | Quando |
|---|---|
| 200 | Alterada |
| 400 | `id` ou campo inválido, JSON malformado |
| 401 | Sem sessão |
| 403 | Estação de outro operador, ou oficial pedida por operador; ou sem token CSRF |
| 404 | Não existe |
| 409 | Estação oficial (pedida por administrador), `external_code` repetido ou chave já usada |
| 415 | Corpo que não é JSON |
| 500 | Erro interno |

---

### `PATCH /api/stations/{id}`

Ativa ou desativa uma estação. Desativada, ela sai do mapa, de `/estacoes` e
de `GET /api/stations`, mas mantém as leituras. **Exige login** e token CSRF.
O operador só muda as suas; o administrador, qualquer uma, **inclusive as
oficiais** (é a única alteração que elas aceitam).

**Corpo**

| Campo | Tipo | Obrigatório |
|---|---|---|
| `active` | booleano | sim |

**Exemplo de requisição**

```sh
curl -X PATCH -b jar.txt -H "X-CSRF-Token: $TOKEN" -H "Content-Type: application/json" \
  -d '{"active":false}' http://localhost:8080/api/stations/3
```

**Exemplo de resposta** — `200 OK`: a estação completa, com `"active": false`
(mesmo formato de `PUT`).

**Erros**

```json
{"error": {"code": "invalid_parameter", "field": "active", "message": "Informe active: true ou false."}}
{"error": {"code": "forbidden", "message": "Você não tem permissão para acessar isto."}}
```

| Status | Quando |
|---|---|
| 200 | Alterada |
| 400 | Sem `active`, `id` inválido, JSON malformado |
| 401 | Sem sessão |
| 403 | Estação de outro operador ou oficial (para operador); sem token CSRF |
| 404 | Não existe |
| 415 | Corpo que não é JSON |
| 500 | Erro interno |

---

### `DELETE /api/stations/{id}`

Exclui uma estação particular **e todas as leituras dela**. **Exige login** e
token CSRF. O operador só exclui as suas; o administrador, qualquer
particular. Estações oficiais não podem ser excluídas (a coleta as criaria de
novo): use `PATCH` para desativá-las.

**Exemplo de requisição**

```sh
curl -i -X DELETE -b jar.txt -H "X-CSRF-Token: $TOKEN" http://localhost:8080/api/stations/3
```

**Exemplo de resposta** — `204 No Content`, sem corpo.

| Status | Quando |
|---|---|
| 204 | Excluída |
| 400 | `id` inválido |
| 401 | Sem sessão |
| 403 | Estação de outro operador, ou oficial pedida por operador; sem token CSRF |
| 404 | Não existe |
| 409 | Estação oficial (pedida por administrador) |
| 500 | Erro interno |

### `POST /api/stations/{id}/readings`

Recebe uma leitura enviada **pela própria estação**. Só para estações
particulares com chave pública cadastrada. Não usa sessão, cookie nem token
CSRF: o pedido é autenticado pela assinatura Ed25519 dos bytes exatos do
corpo, feita com a chave privada da estação. O guia completo, com o preparo
da chave e clientes prontos, está em
[estacao-particular.md](estacao-particular.md).

**Cabeçalhos**

| Nome | Obrigatório | Conteúdo |
|---|---|---|
| `Content-Type` | sim | `application/json` |
| `X-Signature` | sim | Assinatura Ed25519 (64 bytes) dos bytes do corpo, em base64 padrão |

**Corpo** (até 16 KiB; pelo menos um valor medido; campo desconhecido é recusado)

| Campo | Tipo | Regra |
|---|---|---|
| `observed_at` | data RFC 3339 | Opcional; entre 24 h atrás e 5 min à frente do relógio do servidor. Sem ele, a leitura é datada na chegada (segundo cheio) |
| `temperature_c` | número | −90 a 60 |
| `humidity_pct` | número | 0 a 100 |
| `precipitation_mm` | número | 0 a 500 (mm na última hora) |
| `wind_speed_ms` | número | 0 a 120 |
| `wind_dir_deg` | inteiro | 0 a 360 |
| `pressure_hpa` | número | 800 a 1100 |

**Intervalo.** Uma leitura nova antes de passar o intervalo da estação, menos
um décimo de folga, é recusada com 429 `too_soon` e `Retry-After`. A mesma
leitura de novo (mesmo `observed_at`) é aceita com 200 e não é gravada outra
vez: reenviar depois de uma falha de rede é seguro.

**Exemplo de requisição** (assinatura feita pelo cliente
`tools/estacao/enviar-leitura.py` ou pelo firmware)

```sh
curl -i -H "Content-Type: application/json" \
  -H "X-Signature: F86pzshj2tfzjfsBTNKNzAFNTajbZDQchlhO3b1qOUHMUytXlUOhpNxQcVOP3prYSKAtwvl3G93nSBUdudj2DQ==" \
  --data-binary '{"temperature_c":23.4,"humidity_pct":71,"precipitation_mm":0.2}' \
  http://localhost:8080/api/stations/1/readings
```

**Exemplo de resposta** — `201 Created`

```json
{
  "data": {
    "stored": true,
    "observed_at": "2026-11-17T11:16:54Z",
    "next_after": "2026-11-17T11:17:48Z"
  }
}
```

`next_after` é o primeiro instante em que o próximo envio é aceito. Com 200,
`stored` é `false`.

**Erros**

```json
{"error": {"code": "invalid_signature", "message": "Assinatura ausente ou inválida para a chave desta estação."}}
{"error": {"code": "too_soon", "message": "Envio antes do fim do intervalo combinado para esta estação."}}
{"error": {"code": "invalid_parameter", "field": "temperature_c", "message": "A temperatura deve estar entre -90 e 60 °C."}}
{"error": {"code": "uplink_disabled", "message": "Esta estação não está configurada para enviar leituras."}}
```

Outras mensagens de validação: `humidity_pct` "A umidade deve estar entre 0 e
100 %.", `precipitation_mm` "A chuva deve estar entre 0 e 500 mm.",
`wind_speed_ms` "A velocidade do vento deve estar entre 0 e 120 m/s.",
`pressure_hpa` "A pressão deve estar entre 800 e 1100 hPa.", `wind_dir_deg`
"A direção do vento deve ser um número inteiro de 0 a 360 graus.",
`observed_at` "A data da leitura deve estar entre 24 horas atrás e 5 minutos
à frente do relógio do servidor.", `body` "A leitura precisa de pelo menos um
valor medido." ou "O corpo não é um JSON de leitura válido. Confira os nomes e
os tipos dos campos."

| Status | Quando |
|---|---|
| 201 | Leitura gravada |
| 200 | Leitura com o mesmo `observed_at` já gravada (reenvio) |
| 400 | `id` inválido, valor fora da faixa ou JSON inválido (veja `field`) |
| 401 | Sem `X-Signature`, ou assinatura que não confere com a chave da estação |
| 403 | Estação sem chave cadastrada, ou oficial |
| 404 | Estação não existe |
| 413 | Corpo com mais de 16 KiB |
| 415 | Sem `Content-Type: application/json` |
| 429 | Antes do fim do intervalo (`too_soon`), ou mais de 120 pedidos por minuto do mesmo IP (`too_many_requests`); com `Retry-After` |
| 500 | Erro interno |

## Autenticação

Rotas de formulário, usadas pelas páginas `/entrar` e `/cadastro`. Aceitam
envio comum (sem JavaScript) e envio pelo HTMX; o status é o mesmo nos dois.

### `POST /cadastro`

Cria uma conta de **operador** e já abre a sessão. Limite: 10 tentativas por
minuto por IP. Token CSRF obrigatório.

**Campos** (`application/x-www-form-urlencoded`)

| Campo | Obrigatório | Regra |
|---|---|---|
| `nome` | sim | 1 a 100 caracteres |
| `email` | sim | Endereço simples (`ana@example.com`), com domínio que tenha ponto; até 254 caracteres; guardado em minúsculas |
| `senha` | sim | 8 caracteres a 72 bytes |
| `senha-confirma` | sim | Igual a `senha` |
| `termos` | sim | Qualquer valor (caixa marcada) |
| `csrf_token` | sem HTMX | Token CSRF |

**Exemplo de requisição**

```sh
curl -i -c jar.txt -b jar.txt --data-urlencode "csrf_token=$TOKEN" \
  -d "nome=Ana" -d "email=ana@example.com" \
  -d "senha=senha-bem-longa" -d "senha-confirma=senha-bem-longa" -d "termos=on" \
  http://localhost:8080/cadastro
```

**Respostas de sucesso**

| Envio | Resposta |
|---|---|
| Comum | `303 See Other`, `Location: /conta`, `Set-Cookie: climasp_session=...` |
| HTMX | `204 No Content`, `HX-Redirect: /conta`, `Set-Cookie: climasp_session=...` |

**Erros**: envio comum recebe a página de cadastro de novo, com a mensagem e
o que foi digitado (menos a senha); HTMX recebe o fragmento, por exemplo
`<p class="request-error" role="alert" data-field="senha">A senha precisa ter no mínimo 8 caracteres.</p>`.

| Status | `field` | Mensagem |
|---|---|---|
| 400 | `nome` | Informe um nome de até 100 caracteres. |
| 400 | `email` | Informe um e-mail válido. |
| 400 | `senha` | A senha precisa ter no mínimo 8 caracteres. / A senha pode ter no máximo 72 caracteres. |
| 400 | `senha-confirma` | As senhas não são iguais. |
| 400 | `termos` | É preciso concordar que as estações cadastradas ficam públicas. |
| 403 | — | Token CSRF ausente ou diferente |
| 409 | — | Já existe uma conta com este e-mail. |
| 429 | — | Muitas tentativas. Aguarde um minuto e tente de novo. (`Retry-After` em segundos) |

---

### `POST /entrar`

Abre a sessão. Limite: 10 tentativas por minuto por IP. Token CSRF
obrigatório.

**Campos**

| Campo | Obrigatório | Regra |
|---|---|---|
| `email` | sim | E-mail da conta |
| `senha` | sim | Senha |
| `lembrar` | não | Presente: sessão de 30 dias com cookie persistente; ausente: 12 h, cookie apagado ao fechar o navegador |
| `csrf_token` | sem HTMX | Token CSRF |

**Exemplo de requisição**

```sh
curl -i -c jar.txt -b jar.txt --data-urlencode "csrf_token=$TOKEN" \
  -d "email=ana@example.com" -d "senha=senha-bem-longa" -d "lembrar=on" \
  http://localhost:8080/entrar
```

**Respostas de sucesso**: as mesmas de `POST /cadastro` (`303` para
`/conta`, ou `204` com `HX-Redirect: /conta`).

**Erros**

```html
<p class="request-error" role="alert">E-mail ou senha incorretos.</p>
```

| Status | Quando |
|---|---|
| 401 | E-mail inexistente ou senha errada (mesma mensagem para os dois) |
| 403 | Token CSRF ausente ou diferente |
| 429 | Mais de 10 tentativas no minuto; `Retry-After: 60` |

---

### `POST /sair`

Encerra a sessão: apaga a sessão no banco e o cookie. Um cookie antigo deixa
de valer. Token CSRF obrigatório (é POST justamente para que um link em outro
site não consiga deslogar ninguém).

**Campos:** `csrf_token` (sem HTMX).

**Exemplo de requisição**

```sh
curl -i -c jar.txt -b jar.txt -d "csrf_token=$TOKEN" http://localhost:8080/sair
```

| Status | Quando |
|---|---|
| 303 | Saiu; `Location: /`. Também sem sessão aberta |
| 403 | Token CSRF ausente ou diferente |

## Páginas HTML

Respondem `200` com a página inteira (`text/html`).

| Método | Rota | Acesso | Conteúdo | Outros status |
|---|---|---|---|---|
| GET | `/` | todos | Página inicial | — |
| GET | `/mapa` | todos | Mapa interativo e lista de estações | — |
| GET | `/entrar` | todos | Formulário de login | `303` para `/conta` se já logado |
| GET | `/cadastro` | todos | Formulário de cadastro | `303` para `/conta` se já logado |
| GET | `/conta` | logado | Dados da conta | `303` para `/entrar` sem login |
| GET | `/admin/usuarios` | administrador | Lista de contas (até 500) | `303` para `/entrar` sem login; `403` para operador |
| GET | `/painel/estacoes` | logado | [Painel de estações](#painel-de-estações) | `303` para `/entrar`; `400` com `offset` inválido |
| GET | `/painel/estacoes/nova` | logado | Formulário de nova estação | `303` para `/entrar` |
| GET | `/painel/estacoes/{id}/editar` | dono ou administrador | Formulário de edição | `303`, `400`, `403`, `404` |

Uma rota desconhecida responde `404`.

## Fragmentos HTMX

Trechos de HTML que a página troca sem recarregar. Abertos a todos. Erros
voltam como `<p class="request-error" role="alert">...</p>` quando o pedido
tem `HX-Request: true`.

### `GET /estacoes`

Cartões das estações ativas, 12 por vez, para a lista do mapa.

| Parâmetro | Tipo | Padrão | Regra |
|---|---|---|---|
| `offset` | inteiro | 0 | ≥ 0 |

Com `offset=0` vem a lista inteira (linha de status e grade); nas seguintes,
só os cartões. Se houver mais, o último item é um botão que busca a página
seguinte e se troca por ela.

```html
<p class="station-list-status" id="station-list-status">2 estações cadastradas.</p>
<ul class="station-grid" id="station-grid" role="list">
  <li>
    <article class="station-card" id="estacao-1" tabindex="-1">
      <h3>Campinas</h3>
      <p class="station-card-meta">Open-Meteo &middot; confiança baixa</p>
      <dl class="station-readings">
        <dt>Temperatura</dt><dd>24,3 °C</dd>
        <dt>Vento</dt><dd>11 km/h de sudeste</dd>
        <dt>Chuva (1 h)</dt><dd>sem dado</dd>
      </dl>
      <p class="station-card-meta">Atualizado em <time datetime="2026-11-17T07:00:00-03:00">17/11 às 07:00</time></p>
    </article>
  </li>
  ...
</ul>
```

| Status | Quando |
|---|---|
| 200 | Sucesso |
| 400 | `offset` inválido |

### `GET /estacoes/{id}`

Cartão de uma estação ativa, mostrado no balão do mapa.

```html
<article class="station-card">
  <h3>São Paulo - Sé</h3>
  <p class="station-card-meta">CEMADEN &middot; confiança alta</p>
  <dl class="station-readings">
    <dt>Temperatura</dt><dd>sem dado</dd>
    <dt>Vento</dt><dd>sem dado</dd>
    <dt>Chuva (1 h)</dt><dd>1,6 mm</dd>
  </dl>
  <p class="station-card-meta">Atualizado em <time datetime="2026-11-17T07:40:00-03:00">17/11 às 07:40</time></p>
</article>
```

| Status | Quando |
|---|---|
| 200 | Sucesso |
| 400 | `id` inválido |
| 404 | Não existe ou está desativada: `<p class="request-error" role="alert">Recurso não encontrado.</p>` |

## Painel de estações

Rotas usadas por `/painel/estacoes`. Todas exigem login; o serviço aplica as
mesmas regras de permissão da API. Cada ação existe em duas formas: a do HTMX
e a do formulário comum, para quem está sem JavaScript.

| Método | Rota | Campos ou parâmetros | Sucesso com HTMX | Sucesso sem JavaScript |
|---|---|---|---|---|
| GET | `/painel/estacoes/linhas` | `q` (busca, até 120 caracteres), `offset` | `200`: linhas `<tr>` da tabela; com `offset=0`, também a linha de status (fora de banda) e `HX-Replace-Url` | — |
| POST | `/painel/estacoes` | `name`, `external_code`, `lat`, `lon`, `altitude_m`, `active`, `public_key`, `report_interval_s`, `csrf_token` | `204`, `HX-Redirect: /painel/estacoes?aviso=criada` | `303` para a mesma URL |
| PUT | `/painel/estacoes/{id}` | os mesmos | `204`, `HX-Redirect: /painel/estacoes?aviso=alterada` | — |
| POST | `/painel/estacoes/{id}` | os mesmos | — | `303` para `/painel/estacoes?aviso=alterada` |
| POST | `/painel/estacoes/{id}/ativa` | `active=true` ou `false`, `csrf_token` | `200`: a linha `<tr>` nova | `303` para `/painel/estacoes?aviso=ativada` ou `desativada` |
| DELETE | `/painel/estacoes/{id}` | — | `200` vazio (a linha some) | — |
| POST | `/painel/estacoes/{id}/excluir` | `csrf_token` | — | `303` para `/painel/estacoes?aviso=excluida` |

No formulário, `lat`, `lon` e `altitude_m` aceitam vírgula decimal (`-23,55`),
e `active` marcada é qualquer valor. As regras dos campos são as da
[API](#post-apistations); as mensagens de tipo são próprias do formulário:

| `field` | Mensagem |
|---|---|
| `lat` | A latitude deve ser um número, como -23,55. |
| `lon` | A longitude deve ser um número, como -46,63. |
| `altitude_m` | A altitude deve ser um número de metros, como 760. |

Erros com HTMX voltam como fragmento, com o campo em `data-field` (a página
marca e foca o campo):

```html
<p class="request-error" role="alert" data-field="name">Informe o nome da estação.</p>
```

Sem JavaScript, o formulário volta inteiro, com a mensagem, os valores
digitados e o campo marcado com `aria-invalid="true"`. Os status são os da
API: 400, 401, 403, 404, 409.

Exemplo de resposta de uma busca sem resultado
(`GET /painel/estacoes/linhas?q=nada`), com `HX-Replace-Url: /painel/estacoes?q=nada`:

```html
<template>
<p class="panel-status" id="panel-status" role="status" hx-swap-oob="true">Nenhuma estação encontrada para “nada”.</p>
</template>
<tr>
  <td class="panel-empty" colspan="5">
    Nenhuma estação encontrada. Tente outra parte do nome ou do código.
    <a href="/painel/estacoes/nova">Cadastrar uma estação</a>
  </td>
</tr>
```

## Outros

### `GET /healthz`

Estado do servidor e da conexão com o banco, para monitoramento. Aberta a
todos.

```sh
curl http://localhost:8080/healthz
```

| Status | Corpo |
|---|---|
| 200 | `{"status":"ok"}` |
| 503 | `{"status":"unavailable"}` (banco inacessível; a causa vai para o log) |

### `GET /static/...`

Arquivos estáticos de `web/static/`: CSS, JavaScript, bibliotecas (HTMX,
Leaflet), imagens e dados do mapa (`data/sp-geo.js`, `data/sp-municipios.js`,
`data/relevo-sp.js`). `404` para arquivo inexistente.
