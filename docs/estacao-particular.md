# Estação particular: envio automático de leituras

Uma estação particular (cadastrada no site por um operador) pode enviar as
próprias leituras ao ClimaSP, sozinha, a cada intervalo. Serve tanto para um
computador de placa única (Raspberry Pi e similares) quanto para um
microcontrolador (ESP32, RP2040 e similares).

A estação prova quem é com uma **chave Ed25519**, criada pelo `ssh-keygen`.
A chave privada fica só na estação e assina cada leitura; o servidor guarda
apenas a chave pública e confere a assinatura. Nada secreto fica no servidor:
quem ler o banco não consegue enviar leituras em nome de ninguém.

A referência do endpoint está em [api.md](api.md#post-apistationsidreadings).

## 1. Criar a chave

Em qualquer computador com OpenSSH (Linux, macOS, Windows 10 ou mais novo):

```sh
ssh-keygen -t ed25519 -N "" -C "sitio-01" -f estacao
```

Saem dois arquivos:

| Arquivo | O que é | Para onde vai |
|---|---|---|
| `estacao` | Chave privada | Só para a estação. Não envie, não versione |
| `estacao.pub` | Chave pública, uma linha `ssh-ed25519 AAAA... sitio-01` | Para o painel do ClimaSP |

`-N ""` deixa a chave sem senha, porque a estação envia sem ninguém por
perto. Só Ed25519 é aceita: chaves RSA e ECDSA são recusadas.

## 2. Cadastrar a chave no painel

1. Entre no ClimaSP e abra **Minhas estações**.
2. Cadastre a estação, ou clique em **Editar** numa que já existe.
3. Em **Envio automático**, cole a linha de `estacao.pub` em **Chave pública**.
4. Escolha o **Intervalo de envio** (de 1 minuto a 24 horas) e salve.

A página de edição passa a mostrar a impressão digital da chave (a mesma que
`ssh-keygen -lf estacao.pub` mostra), o **endereço de envio** e o último
envio recebido. No painel, a estação aparece como "aguardando o 1º envio".

Pela API, os campos são `public_key` e `report_interval_s` (em segundos) em
`POST` ou `PUT /api/stations`.

## 3. O protocolo

Cada leitura é um `POST` com o JSON da leitura no corpo e a assinatura dele
num cabeçalho:

```
POST /api/stations/{id}/readings
Content-Type: application/json
X-Signature: <base64 da assinatura Ed25519 dos bytes do corpo>

{"observed_at":"2026-11-17T11:16:54Z","temperature_c":23.4,"humidity_pct":71,"precipitation_mm":0.2}
```

Regras:

- **A assinatura cobre os bytes exatos do corpo.** Monte o JSON uma vez,
  assine esses bytes e envie esses mesmos bytes, sem reformatar.
- **Assinatura Ed25519 pura**, 64 bytes, em base64 padrão (com `+`, `/` e
  `=`). É a saída de qualquer biblioteca Ed25519 (libsodium, Monocypher,
  TweetNaCl, `cryptography` do Python). Não é o envelope SSHSIG de
  `ssh-keygen -Y sign`.
- **Sem cookie, sem login, sem token CSRF.** A assinatura é a autenticação.
- **Campos** (pelo menos um valor medido; campo desconhecido é recusado):

| Campo | Unidade | Faixa aceita |
|---|---|---|
| `observed_at` | data RFC 3339 | opcional; de 24 h atrás a 5 min à frente do relógio do servidor. Sem ele, o servidor data a leitura na chegada |
| `temperature_c` | °C | −90 a 60 |
| `humidity_pct` | % | 0 a 100 |
| `precipitation_mm` | mm na última hora | 0 a 500 |
| `wind_speed_ms` | m/s | 0 a 120 |
| `wind_dir_deg` | graus, de onde o vento vem | inteiro de 0 a 360 |
| `pressure_hpa` | hPa | 800 a 1100 |

Respostas:

| Status | Significado | O que a estação faz |
|---|---|---|
| 201 | Leitura gravada | Espera até `next_after` |
| 200 | Já havia uma leitura com o mesmo horário (reenvio) | Nada: não é erro |
| 400 | Valor fora da faixa ou JSON inválido (`field` diz qual) | Corrigir o firmware ou o sensor |
| 401 | Assinatura ausente ou não confere com a chave cadastrada | Conferir a chave |
| 403 | A estação não tem chave cadastrada | Cadastrar a chave no painel |
| 404 | A estação não existe | Conferir o id |
| 413 | Corpo maior que 16 KiB | Enviar uma leitura por vez |
| 415 | Faltou `Content-Type: application/json` | Corrigir o cabeçalho |
| 429 | Antes do fim do intervalo (`too_soon`), ou mais de 120 pedidos por minuto do mesmo IP | Esperar os segundos de `Retry-After` |

Exemplo de resposta `201`:

```json
{
  "data": {
    "stored": true,
    "observed_at": "2026-11-17T11:16:54Z",
    "next_after": "2026-11-17T11:17:48Z"
  }
}
```

### Intervalo

O servidor recusa (429) uma leitura nova antes de passar o intervalo
combinado, com uma folga de um décimo para o relógio do dispositivo e o
tempo da rede: com intervalo de 10 minutos, aceita a partir de 9. A resposta
traz o horário a partir do qual o próximo envio é aceito (`next_after`), e a
recusa traz `Retry-After`.

Se a estação ficar **dois intervalos** sem enviar, o painel a mostra como
"envio atrasado".

### Reenvio é seguro

Se a estação não receber a resposta (rede caiu no meio), pode mandar a mesma
leitura de novo: com o mesmo `observed_at`, o servidor responde 200 e não
grava duas vezes. Por isso vale mandar `observed_at` quando há relógio (NTP).
Alguém que capture um envio e o repita também não consegue nada: a leitura já
está gravada com aquele horário.

### Trocar a chave

Cole a nova chave pública no painel e salve. A anterior deixa de valer na
hora, e a contagem do intervalo recomeça. Faça isso se a chave privada vazar
ou se o dispositivo for trocado. Deixar o campo vazio desliga o envio.

## 4a. Computador de placa única (Linux)

Use o cliente pronto, [`tools/estacao/enviar-leitura.py`](../tools/estacao/enviar-leitura.py):

```sh
pip install cryptography
mkdir -p ~/.climasp && mv estacao estacao.pub ~/.climasp/

python3 enviar-leitura.py --servidor https://climasp.exemplo.br --estacao 42 \
    --chave ~/.climasp/estacao --temperatura 23.4 --umidade 71 --chuva 0.2
```

```
Leitura gravada (2026-11-17T11:17:18Z). Proximo envio a partir de 2026-11-17T11:18:13Z.
```

Sai com código 0 quando a leitura foi aceita, 1 quando o servidor recusou e 2
sem rede. `--sem-horario` deixa o servidor datar a leitura (para quem não tem
relógio confiável), e `--verbose` mostra o corpo e a assinatura.

Para enviar sozinho a cada 10 minutos, um timer do systemd. Troque
`ler-sensores.sh` pelo seu programa, que lê os sensores e chama o cliente:

```ini
# /etc/systemd/system/climasp.service
[Unit]
Description=Envia uma leitura ao ClimaSP
After=network-online.target

[Service]
Type=oneshot
User=pi
ExecStart=/home/pi/climasp/ler-sensores.sh
```

```ini
# /etc/systemd/system/climasp.timer
[Unit]
Description=Envio periodico ao ClimaSP

[Timer]
OnBootSec=1min
OnUnitActiveSec=10min

[Install]
WantedBy=timers.target
```

```sh
sudo systemctl enable --now climasp.timer
```

Ou, mais simples, uma linha no `crontab -e`:

```
*/10 * * * * /home/pi/climasp/ler-sensores.sh >> /home/pi/climasp/envio.log 2>&1
```

## 4b. Microcontrolador

Um microcontrolador não lê o formato de chave do OpenSSH; a biblioteca
Ed25519 dele precisa só da **semente** de 32 bytes da chave privada. Converta
no computador onde criou a chave:

```sh
pip install cryptography
python3 tools/estacao/chave-para-mcu.py estacao > chave_estacao.h
```

`chave_estacao.h` define `CLIMASP_SEED`, a semente como vetor em C, e traz a
chave pública num comentário. **É a chave privada**: não versione (o
`.gitignore` do projeto já ignora esse nome).

O firmware faz, a cada intervalo:

1. Lê os sensores e, se tiver NTP, o horário em UTC.
2. Monta o JSON num buffer.
3. Assina os bytes do buffer com a chave gerada da semente.
4. Codifica a assinatura em base64.
5. Faz o `POST` com `Content-Type` e `X-Signature`.
6. Com 201 ou 200, dorme até o próximo intervalo; com 429, espera
   `Retry-After`; com outro erro, registra e tenta no próximo ciclo.

O exemplo [`tools/estacao/mcu/assinar_leitura.c`](../tools/estacao/mcu/assinar_leitura.c)
faz os passos 2 a 4 com a libsodium, que o ESP-IDF traz como componente
(`idf.py add-dependency espressif/libsodium`):

```c
unsigned char publica[crypto_sign_PUBLICKEYBYTES];
unsigned char privada[crypto_sign_SECRETKEYBYTES];
crypto_sign_seed_keypair(publica, privada, CLIMASP_SEED);   /* uma vez */

int n = snprintf(corpo, sizeof corpo,
                 "{\"temperature_c\":%.1f,\"humidity_pct\":%.0f}", t, u);
crypto_sign_detached(assinatura, NULL, (unsigned char *)corpo, n, privada);
sodium_bin2base64(assinatura_b64, sizeof assinatura_b64, assinatura,
                  crypto_sign_BYTES, sodium_base64_VARIANT_ORIGINAL);
```

O mesmo arquivo compila num computador comum, para testar a assinatura antes
de gravar o firmware:

```sh
cd tools/estacao/mcu
python3 ../chave-para-mcu.py ~/estacao > chave_estacao.h
cc assinar_leitura.c -lsodium -o assinar_leitura
./assinar_leitura   # linha 1: corpo; linha 2: assinatura
```

Com outra biblioteca Ed25519 a troca é direta: a semente gera o par de
chaves, e a assinatura de uma mensagem é de 64 bytes. O envio HTTP usa o
cliente da plataforma (no ESP32, `esp_http_client`; no Arduino, `HTTPClient`).
Use HTTPS quando o servidor tiver certificado: a assinatura impede que a
leitura seja alterada, mas não a esconde.

## 5. Testar sem a estação

Com o cliente Python, de qualquer computador, usando a chave privada:

```sh
python3 tools/estacao/enviar-leitura.py --servidor http://localhost:8080 \
    --estacao 1 --chave estacao --temperatura 22 --verbose
```

Ou à mão, com `openssl` (3.0 ou mais novo) e `curl`, para ver o protocolo cru.
O `openssl` não lê o formato de chave do OpenSSH; a primeira linha grava uma
cópia em PKCS#8 (`estacao.pem`, também secreta):

```sh
python3 -c "import sys; from cryptography.hazmat.primitives import serialization as s; \
k = s.load_ssh_private_key(open('estacao', 'rb').read(), None); \
open('estacao.pem', 'wb').write(k.private_bytes(s.Encoding.PEM, s.PrivateFormat.PKCS8, s.NoEncryption()))"

printf '%s' '{"temperature_c":22.0}' > corpo.json
openssl pkeyutl -sign -inkey estacao.pem -rawin -in corpo.json | base64 -w0 > assinatura
curl -i -H "Content-Type: application/json" -H "X-Signature: $(cat assinatura)" \
    --data-binary @corpo.json http://localhost:8080/api/stations/1/readings
```
