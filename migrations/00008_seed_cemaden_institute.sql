-- +goose Up
-- Pluviometros automaticos do CEMADEN. ON CONFLICT porque db/seed_dev.sql
-- tambem cadastra este instituto, e pode ter rodado antes.
INSERT INTO institutes (code, name, website) VALUES
    ('cemaden', 'CEMADEN', 'https://www.gov.br/cemaden')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
-- As estacoes coletadas deste instituto saem antes dele: a chave estrangeira
-- de stations para institutes e ON DELETE RESTRICT, de proposito, para que um
-- instituto nao seja apagado por engano. As observacoes vao junto com as
-- estacoes (ON DELETE CASCADE).
DELETE FROM stations WHERE institute_id = (SELECT id FROM institutes WHERE code = 'cemaden');
DELETE FROM institutes WHERE code = 'cemaden';
