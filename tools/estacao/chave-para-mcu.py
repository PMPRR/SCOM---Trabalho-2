#!/usr/bin/env python3
"""Converte a chave do ssh-keygen para gravar num microcontrolador.

Um microcontrolador (ESP32, RP2040...) nao le o formato do OpenSSH. O que a
biblioteca Ed25519 dele precisa e a semente de 32 bytes da chave privada; a
chave publica sai dela. Este script le a chave criada com

    ssh-keygen -t ed25519 -N "" -f estacao

e imprime a semente como um vetor em C, para colar no firmware.

    python3 chave-para-mcu.py estacao > chave_estacao.h

A semente E a chave privada: quem a tiver assina leituras em nome da
estacao. Nao a coloque em repositorio. Se vazar, gere outra chave e troque
a chave publica no painel; a anterior deixa de valer na hora.
"""

import sys
from pathlib import Path

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import (
        Encoding, NoEncryption, PrivateFormat, PublicFormat, load_ssh_private_key,
    )
except ImportError:
    sys.exit("Instale a biblioteca cryptography: pip install cryptography")


def main():
    if len(sys.argv) != 2:
        sys.exit("uso: chave-para-mcu.py ARQUIVO_DA_CHAVE_PRIVADA")
    chave = load_ssh_private_key(Path(sys.argv[1]).read_bytes(), password=None)
    if not isinstance(chave, Ed25519PrivateKey):
        sys.exit("A chave precisa ser Ed25519 (ssh-keygen -t ed25519).")

    semente = chave.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption())
    publica = chave.public_key().public_bytes(Encoding.OpenSSH, PublicFormat.OpenSSH).decode()

    print("/* Chave da estacao do ClimaSP. SEGREDO: nao versionar. */")
    print("/* Chave publica (cadastrada no painel): %s */" % publica)
    print("#define CLIMASP_SEED_LEN 32")
    print("static const unsigned char CLIMASP_SEED[CLIMASP_SEED_LEN] = {")
    for k in range(0, 32, 8):
        print("    " + ", ".join("0x%02x" % b for b in semente[k:k + 8]) + ",")
    print("};")


if __name__ == "__main__":
    main()
