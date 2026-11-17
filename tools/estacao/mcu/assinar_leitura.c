/*
 * Assinatura de uma leitura para o ClimaSP, em C, para microcontrolador.
 *
 * Mostra a parte que importa num firmware: montar o JSON, assinar os bytes
 * exatos com Ed25519 e codificar a assinatura em base64 para o cabecalho
 * X-Signature. O envio HTTP fica com o cliente HTTP da plataforma (no
 * ESP32, esp_http_client; no Arduino, HTTPClient), e o horario, com o NTP.
 *
 * Usa a libsodium, que o ESP-IDF traz como componente (idf.py add-dependency
 * espressif/libsodium). Em outra biblioteca Ed25519 a troca e direta: a
 * semente de 32 bytes gera o par de chaves, e a assinatura e de 64 bytes.
 *
 * Neste computador, para testar:
 *   python3 ../chave-para-mcu.py CHAVE > chave_estacao.h
 *   cc assinar_leitura.c -lsodium -o assinar_leitura
 *   ./assinar_leitura   # imprime o corpo e a assinatura
 */
#include <stdio.h>
#include <string.h>
#include <sodium.h>

#include "chave_estacao.h" /* CLIMASP_SEED, gerado por chave-para-mcu.py */

/* Monta o corpo. Sem relogio confiavel, omita observed_at: o servidor data
 * a leitura na chegada. Com NTP, inclua o horario em UTC. */
static int montar_corpo(char *saida, size_t tamanho, float temperatura, float umidade, float chuva) {
    return snprintf(saida, tamanho,
                    "{\"temperature_c\":%.1f,\"humidity_pct\":%.0f,\"precipitation_mm\":%.1f}",
                    temperatura, umidade, chuva);
}

int main(void) {
    if (sodium_init() < 0) {
        return 1;
    }

    /* O par de chaves sai da semente, uma vez, na inicializacao. */
    unsigned char publica[crypto_sign_PUBLICKEYBYTES];
    unsigned char privada[crypto_sign_SECRETKEYBYTES];
    crypto_sign_seed_keypair(publica, privada, CLIMASP_SEED);

    char corpo[256];
    int n = montar_corpo(corpo, sizeof corpo, 23.4f, 71.0f, 0.2f);
    if (n < 0 || (size_t)n >= sizeof corpo) {
        return 1;
    }

    /* A assinatura cobre os bytes exatos do corpo, que vao sem mudanca. */
    unsigned char assinatura[crypto_sign_BYTES];
    crypto_sign_detached(assinatura, NULL, (const unsigned char *)corpo, (size_t)n, privada);

    char assinatura_b64[sodium_base64_ENCODED_LEN(crypto_sign_BYTES, sodium_base64_VARIANT_ORIGINAL)];
    sodium_bin2base64(assinatura_b64, sizeof assinatura_b64, assinatura, sizeof assinatura,
                      sodium_base64_VARIANT_ORIGINAL);

    /* No firmware: POST /api/stations/{id}/readings com
     *   Content-Type: application/json
     *   X-Signature: <assinatura_b64>
     * e o corpo. 201 = gravada; 200 = repetida; 429 = antes do intervalo
     * (respeite Retry-After); 401 = assinatura nao confere com a chave. */
    printf("%s\n%s\n", corpo, assinatura_b64);

    sodium_memzero(privada, sizeof privada);
    return 0;
}
