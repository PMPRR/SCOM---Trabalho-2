-- +goose Up
-- Dono da estacao: o operador que a cadastrou. NULL = estacao oficial de um
-- instituto, gerida apenas por administradores.
ALTER TABLE stations
    ADD COLUMN owner_id BIGINT REFERENCES users (id) ON DELETE SET NULL;

CREATE INDEX stations_owner_id_idx ON stations (owner_id);

-- +goose Down
ALTER TABLE stations DROP COLUMN owner_id;

