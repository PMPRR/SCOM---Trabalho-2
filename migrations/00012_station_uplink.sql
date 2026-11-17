-- +goose Up
-- Envio automatico de leituras por estacoes particulares. A estacao assina
-- cada envio com a chave privada Ed25519 dela; o servidor guarda so a chave
-- publica, no formato do OpenSSH ("ssh-ed25519 AAAA..."), e a impressao
-- digital SHA-256 dela, que e unica: uma chave identifica uma estacao.
-- As tres colunas existem juntas ou nenhuma: sem chave, a estacao nao
-- recebe envios; com chave, tem um intervalo combinado.
ALTER TABLE stations
    ADD COLUMN public_key        TEXT CHECK (public_key ~ '^ssh-ed25519 [A-Za-z0-9+/]+={0,2}$'),
    ADD COLUMN key_fingerprint   TEXT CHECK (key_fingerprint ~ '^SHA256:[A-Za-z0-9+/]{43}$'),
    ADD COLUMN report_interval_s INTEGER CHECK (report_interval_s BETWEEN 60 AND 86400),
    ADD COLUMN last_report_at    TIMESTAMPTZ,
    ADD CONSTRAINT stations_key_fingerprint_key UNIQUE (key_fingerprint),
    ADD CONSTRAINT stations_uplink_check CHECK (
        (public_key IS NULL) = (key_fingerprint IS NULL)
        AND (public_key IS NULL) = (report_interval_s IS NULL)
    );

-- +goose Down
ALTER TABLE stations
    DROP CONSTRAINT stations_uplink_check,
    DROP CONSTRAINT stations_key_fingerprint_key,
    DROP COLUMN last_report_at,
    DROP COLUMN report_interval_s,
    DROP COLUMN key_fingerprint,
    DROP COLUMN public_key;
