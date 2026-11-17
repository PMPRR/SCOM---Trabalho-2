-- +goose Up
-- A fonte METAR (aeroportos, via NOAA) saiu do coletor. O instituto que a
-- migracao 00007 cadastrou deixa de ter uso, e sai junto com as estacoes e
-- leituras que ja tenham sido coletadas dele (as observacoes vao por
-- ON DELETE CASCADE).
DELETE FROM stations WHERE institute_id = (SELECT id FROM institutes WHERE code = 'metar');
DELETE FROM institutes WHERE code = 'metar';

-- +goose Down
INSERT INTO institutes (code, name, website) VALUES
    ('metar', 'METAR dos aeroportos (DECEA, via NOAA)', 'https://aviationweather.gov')
ON CONFLICT (code) DO NOTHING;
