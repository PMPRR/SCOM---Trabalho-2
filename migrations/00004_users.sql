-- +goose Up
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT        NOT NULL UNIQUE CHECK (length(email) BETWEEN 3 AND 254),
    name          TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('operator', 'admin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
