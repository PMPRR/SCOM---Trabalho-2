#!/usr/bin/env python3
"""Envia uma leitura de uma estacao particular para o ClimaSP.

Para estacoes com Linux (Raspberry Pi e outros computadores de placa unica).
A leitura vai em JSON, assinada com a chave privada Ed25519 da estacao: o
servidor confere a assinatura com a chave publica cadastrada no painel.

Preparo, uma vez, na estacao:

    ssh-keygen -t ed25519 -N "" -C "minha-estacao" -f ~/.climasp/estacao
    pip install cryptography

Cole o conteudo de ~/.climasp/estacao.pub no painel (Editar estacao >
Envio automatico) e escolha o intervalo. Depois, a cada intervalo:

    python3 enviar-leitura.py --servidor https://climasp.exemplo.br \\
        --estacao 42 --chave ~/.climasp/estacao \\
        --temperatura 23.4 --umidade 71 --chuva 0.2

Codigos de saida: 0 leitura aceita (nova ou repetida), 1 recusada pelo
servidor, 2 erro de uso ou de rede. Com --verbose, mostra o que foi enviado.
"""

import argparse
import base64
import json
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import load_ssh_private_key
except ImportError:
    sys.exit("Instale a biblioteca cryptography: pip install cryptography")

# Opcao da linha de comando -> campo do JSON
CAMPOS = {
    "temperatura": "temperature_c",
    "umidade": "humidity_pct",
    "chuva": "precipitation_mm",
    "vento": "wind_speed_ms",
    "direcao": "wind_dir_deg",
    "pressao": "pressure_hpa",
}


def carregar_chave(caminho):
    dados = Path(caminho).expanduser().read_bytes()
    chave = load_ssh_private_key(dados, password=None)
    if not isinstance(chave, Ed25519PrivateKey):
        sys.exit("A chave precisa ser Ed25519 (ssh-keygen -t ed25519).")
    return chave


def montar_corpo(args):
    leitura = {}
    for opcao, campo in CAMPOS.items():
        valor = getattr(args, opcao)
        if valor is not None:
            leitura[campo] = int(valor) if campo == "wind_dir_deg" else valor
    if not leitura:
        sys.exit("Informe pelo menos um valor medido (--temperatura, --chuva...).")
    if not args.sem_horario:
        # O horario da medicao, em UTC. Sem relogio confiavel (sem NTP), use
        # --sem-horario e o servidor data a leitura na chegada.
        leitura["observed_at"] = datetime.now(timezone.utc).replace(microsecond=0).isoformat()
    # Os bytes enviados sao exatamente os assinados.
    return json.dumps(leitura, separators=(",", ":")).encode("utf-8")


def main():
    parser = argparse.ArgumentParser(description="Envia uma leitura assinada ao ClimaSP.")
    parser.add_argument("--servidor", required=True, help="endereco do servidor, ex.: http://192.168.0.10:8080")
    parser.add_argument("--estacao", required=True, type=int, help="id da estacao (aparece no painel)")
    parser.add_argument("--chave", required=True, help="chave privada Ed25519 criada pelo ssh-keygen")
    parser.add_argument("--temperatura", type=float, help="graus Celsius")
    parser.add_argument("--umidade", type=float, help="umidade relativa, %%")
    parser.add_argument("--chuva", type=float, help="mm na ultima hora")
    parser.add_argument("--vento", type=float, help="velocidade do vento, m/s")
    parser.add_argument("--direcao", type=float, help="de onde o vento vem, graus (0 a 360)")
    parser.add_argument("--pressao", type=float, help="hPa")
    parser.add_argument("--sem-horario", action="store_true", help="deixa o servidor datar a leitura")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    chave = carregar_chave(args.chave)
    corpo = montar_corpo(args)
    assinatura = base64.b64encode(chave.sign(corpo)).decode("ascii")

    url = "%s/api/stations/%d/readings" % (args.servidor.rstrip("/"), args.estacao)
    pedido = urllib.request.Request(url, data=corpo, method="POST", headers={
        "Content-Type": "application/json",
        "X-Signature": assinatura,
        "User-Agent": "climasp-estacao/1.0",
    })
    if args.verbose:
        print("POST", url)
        print("corpo:", corpo.decode())
        print("assinatura:", assinatura)

    try:
        with urllib.request.urlopen(pedido, timeout=30) as resposta:
            dados = json.load(resposta)["data"]
            situacao = "gravada" if dados["stored"] else "ja estava gravada"
            print("Leitura %s (%s). Proximo envio a partir de %s." % (
                situacao, dados["observed_at"], dados["next_after"]))
            return 0
    except urllib.error.HTTPError as erro:
        try:
            mensagem = json.load(erro)["error"]["message"]
        except (ValueError, KeyError):
            mensagem = erro.reason
        espera = erro.headers.get("Retry-After")
        extra = " Tente de novo em %s s." % espera if espera else ""
        print("Recusada (%d): %s%s" % (erro.code, mensagem, extra), file=sys.stderr)
        return 1
    except urllib.error.URLError as erro:
        print("Sem conexao com o servidor: %s" % erro.reason, file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
