-- +goose Up
-- Instituto das estacoes cadastradas no site pelos operadores. Toda estacao
-- pertence a um instituto (stations.institute_id e NOT NULL); as cadastradas
-- a mao ficam neste, e quem as cadastrou fica em stations.owner_id.
-- O codigo de cada uma e unico dentro do instituto, pela UNIQUE
-- (institute_id, external_code) que ja existe em stations.
INSERT INTO institutes (code, name) VALUES ('particular', 'Estação particular')
ON CONFLICT (code) DO NOTHING;

-- O painel lista as estacoes de um operador da mais nova para a mais antiga.
CREATE INDEX stations_owner_id_id_idx ON stations (owner_id, id DESC);
DROP INDEX stations_owner_id_idx;

-- +goose Down
-- As estacoes do instituto saem antes dele: a chave estrangeira de stations
-- para institutes e ON DELETE RESTRICT. As observacoes vao junto (CASCADE).
CREATE INDEX stations_owner_id_idx ON stations (owner_id);
DROP INDEX stations_owner_id_id_idx;
DELETE FROM stations WHERE institute_id = (SELECT id FROM institutes WHERE code = 'particular');
DELETE FROM institutes WHERE code = 'particular';
