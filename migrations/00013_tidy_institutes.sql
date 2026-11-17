-- +goose Up
-- O instituto 'inmet' foi cadastrado pela 00005, quando o INMET era uma das
-- fontes da coleta. A fonte saiu do codigo e o instituto ficou orfao. So sai
-- se nao tiver estacoes: um banco de desenvolvimento pode ter as estacoes
-- simuladas do seed_dev.sql, que continuam valendo.
DELETE FROM institutes i
WHERE i.code = 'inmet'
  AND NOT EXISTS (SELECT 1 FROM stations s WHERE s.institute_id = i.id);

-- Bancos de desenvolvimento antigos: o seed_dev.sql punha as estacoes com
-- dono nos institutos 'privada' e 'colaborativa'. Desde a 00011, estacao
-- cadastrada por um operador e do instituto 'particular'. Uma estacao cujo
-- codigo ja exista em 'particular' fica onde esta, e o instituto antigo so
-- e apagado quando fica vazio.
UPDATE stations s
SET institute_id = p.id
FROM institutes p, institutes o
WHERE p.code = 'particular'
  AND o.code IN ('privada', 'colaborativa')
  AND s.institute_id = o.id
  AND NOT EXISTS (
      SELECT 1 FROM stations x WHERE x.institute_id = p.id AND x.external_code = s.external_code
  );

DELETE FROM institutes i
WHERE i.code IN ('privada', 'colaborativa')
  AND NOT EXISTS (SELECT 1 FROM stations s WHERE s.institute_id = i.id);

-- +goose Down
-- Volta so o 'inmet', como a 00005 o deixava. As estacoes movidas para
-- 'particular' ficam la: era la que deviam estar.
INSERT INTO institutes (code, name, website) VALUES
    ('inmet', 'Instituto Nacional de Meteorologia', 'https://portal.inmet.gov.br')
ON CONFLICT (code) DO NOTHING;
