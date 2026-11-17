-- +goose Up
-- Boletins METAR dos aeroportos: emitidos pelo DECEA e lidos, ja decodificados,
-- na API publica do NOAA Aviation Weather Center.
INSERT INTO institutes (code, name, website) VALUES
    ('metar', 'METAR dos aeroportos (DECEA, via NOAA)', 'https://aviationweather.gov');

-- +goose Down
-- As estacoes coletadas deste instituto saem antes dele: a chave estrangeira
-- de stations para institutes e ON DELETE RESTRICT, de proposito, para que um
-- instituto nao seja apagado por engano. As observacoes vao junto com as
-- estacoes (ON DELETE CASCADE).
DELETE FROM stations WHERE institute_id = (SELECT id FROM institutes WHERE code = 'metar');
DELETE FROM institutes WHERE code = 'metar';
