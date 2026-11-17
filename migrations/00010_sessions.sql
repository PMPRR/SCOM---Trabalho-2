-- +goose Up
-- Sessoes de login. O cookie do navegador guarda um token aleatorio; aqui
-- fica so o SHA-256 dele, de modo que quem ler esta tabela nao consegue
-- se passar por ninguem. Apagar a linha encerra a sessao (logout), e apagar
-- o usuario encerra todas as dele.
CREATE TABLE sessions (
    token_hash BYTEA       PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
