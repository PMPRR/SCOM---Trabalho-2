-- +goose Up
INSERT INTO institutes (code, name, website) VALUES
    ('inmet',     'Instituto Nacional de Meteorologia', 'https://portal.inmet.gov.br'),
    ('openmeteo', 'Open-Meteo',                         'https://open-meteo.com');


-- +goose Down
DELETE FROM institutes WHERE code IN ('inmet', 'openmeteo');
