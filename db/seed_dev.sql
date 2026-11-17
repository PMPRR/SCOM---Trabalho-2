-- Dados de DESENVOLVIMENTO para testar o mapa. Nao e uma migracao: nao rode
-- em producao. Uso: make seed-dev
--
-- Sao as 28 estacoes simuladas do Trabalho I (o mesmo quadro sinotico: frente
-- vinda do sul, interior noroeste quente e seco), agora gravadas no banco.
-- As leituras sao datadas em relacao a hora atual, entao o conjunto parece
-- recente sempre que o script roda. Pode ser executado mais de uma vez.

BEGIN;

INSERT INTO institutes (code, name, website) VALUES
    ('cemaden',      'CEMADEN',                'https://www.gov.br/cemaden'),
    ('ciiagro',      'IAC/CIIAGRO',            'http://www.ciiagro.sp.gov.br'),
    ('colaborativa', 'Rede colaborativa',      NULL),
    ('privada',      'Estação privada',        NULL)
ON CONFLICT (code) DO NOTHING;

-- Dono das estacoes privadas. O hash '!' nao corresponde a nenhuma senha,
-- entao esta conta existe so para satisfazer a chave estrangeira e nao
-- consegue fazer login.
INSERT INTO users (email, name, password_hash, role) VALUES
    ('operador.dev@example.com', 'Operador de desenvolvimento', '!', 'operator')
ON CONFLICT (email) DO NOTHING;

INSERT INTO stations (institute_id, external_code, name, location, owner_id)
SELECT i.id,
       v.external_code,
       v.name,
       ST_SetSRID(ST_MakePoint(v.lon, v.lat), 4326)::geography,
       CASE WHEN v.private
            THEN (SELECT id FROM users WHERE email = 'operador.dev@example.com')
       END
FROM (VALUES
    ('colaborativa', 'jal-01', 'Jales - Centro',                   -20.2687, -50.5456, true),
    ('inmet',        'srp-01', 'São José do Rio Preto',            -20.8113, -49.3758, false),
    ('ciiagro',      'ara-01', 'Araçatuba - IAC',                  -21.2079, -50.4325, false),
    ('colaborativa', 'pep-01', 'Presidente Epitácio',              -21.7633, -52.1097, true),
    ('inmet',        'ppr-01', 'Presidente Prudente',              -22.1207, -51.3889, false),
    ('privada',      'mar-01', 'Marília - Zona Norte',             -22.2171, -49.9501, true),
    ('ciiagro',      'bar-01', 'Barretos - IAC',                   -20.5570, -48.5695, false),
    ('colaborativa', 'rpt-01', 'Ribeirão Preto - Centro',          -21.1775, -47.8103, true),
    ('inmet',        'fra-01', 'Franca - Aeroporto',               -20.5390, -47.4010, false),
    ('inmet',        'bau-01', 'Bauru - Aeroporto',                -22.3145, -49.0606, false),
    ('ciiagro',      'arq-01', 'Araraquara - IAC',                 -21.7845, -48.1780, false),
    ('privada',      'scr-01', 'São Carlos - Campus',              -22.0087, -47.8909, true),
    ('ciiagro',      'pir-01', 'Piracicaba - ESALQ',               -22.7253, -47.6492, false),
    ('privada',      'cps-01', 'Campinas - Barão Geraldo',         -22.8184, -47.0647, true),
    ('inmet',        'our-01', 'Ourinhos - Aeroporto',             -22.9795, -49.8695, false),
    ('privada',      'ava-01', 'Avaré - Represa',                  -23.0986, -48.9263, true),
    ('privada',      'sor-01', 'Sorocaba - ICTS',                  -23.5015, -47.4526, true),
    ('inmet',        'spo-01', 'São Paulo - Mirante de Santana',   -23.4961, -46.6199, false),
    ('colaborativa', 'itp-01', 'Itapetininga - Centro',            -23.5917, -48.0533, true),
    ('privada',      'bgp-01', 'Bragança Paulista',                -22.9526, -46.5417, true),
    ('inmet',        'sjc-01', 'São José dos Campos',              -23.1896, -45.8841, false),
    ('privada',      'tau-01', 'Taubaté - Bairro do Barreiro',     -23.0264, -45.5553, true),
    ('inmet',        'cjo-01', 'Campos do Jordão',                 -22.7397, -45.5914, false),
    ('cemaden',      'uba-01', 'Ubatuba - Itaguá',                 -23.4336, -45.0838, false),
    ('inmet',        'san-01', 'Santos - Ponta da Praia',          -23.9608, -46.3336, false),
    ('inmet',        'reg-01', 'Registro - Vale do Ribeira',       -24.4871, -47.8442, false),
    ('cemaden',      'can-01', 'Cananéia - Porto',                 -25.0148, -47.9268, false),
    ('ciiagro',      'ite-01', 'Itapeva - IAC',                    -23.9821, -48.8758, false)
) AS v (institute, external_code, name, lat, lon, private)
JOIN institutes i ON i.code = v.institute
ON CONFLICT (institute_id, external_code) DO NOTHING;

-- age_h: ha quantas horas a leitura foi feita. wind_ms ja esta em m/s.
INSERT INTO observations
    (station_id, observed_at, temperature_c, precipitation_mm, wind_speed_ms, wind_dir_deg)
SELECT s.id,
       date_trunc('hour', now()) - make_interval(hours => v.age_h),
       v.temperature_c,
       v.precipitation_mm,
       v.wind_ms,
       v.wind_dir
FROM (VALUES
    ('colaborativa', 'jal-01', 2, 33.1, 0.0, 1.67, 80),
    ('inmet',        'srp-01', 0, 32.8, 0.0, 2.22, 85),
    ('ciiagro',      'ara-01', 0, 32.4, 0.0, 1.39, 95),
    ('colaborativa', 'pep-01', 3, 32.1, 0.0, 3.06, 105),
    ('inmet',        'ppr-01', 0, 31.2, 0.0, 2.50, 115),
    ('privada',      'mar-01', 1, 29.6, 0.0, 1.94, 130),
    ('ciiagro',      'bar-01', 0, 31.9, 0.0, 1.67, 100),
    ('colaborativa', 'rpt-01', 4, 29.8, 0.0, 1.11, 110),
    ('inmet',        'fra-01', 0, 27.4, 0.0, 3.33, 105),
    ('inmet',        'bau-01', 0, 30.1, 0.0, 2.22, 125),
    ('ciiagro',      'arq-01', 0, 30.5, 0.0, 1.67, 120),
    ('privada',      'scr-01', 0, 27.8, 0.0, 2.78, 130),
    ('ciiagro',      'pir-01', 1, 28.6, 0.0, 1.94, 135),
    ('privada',      'cps-01', 1, 26.3, 0.0, 1.94, 140),
    ('inmet',        'our-01', 0, 29.0, 0.0, 2.50, 165),
    ('privada',      'ava-01', 0, 27.6, 0.0, 2.22, 170),
    ('privada',      'sor-01', 0, 24.1, 0.0, 3.33, 160),
    ('inmet',        'spo-01', 0, 22.7, 1.2, 2.50, 150),
    ('colaborativa', 'itp-01', 3, 25.4, 0.4, 4.72, 200),
    ('privada',      'bgp-01', 0, 24.8, 0.2, 1.39, 145),
    ('inmet',        'sjc-01', 0, 24.3, 0.6, 1.94, 145),
    ('privada',      'tau-01', 0, 25.2, 0.8, 1.67, 150),
    ('inmet',        'cjo-01', 0, 17.9, 2.4, 9.44, 175),
    ('cemaden',      'uba-01', 0, 24.0, 5.2, 6.67, 155),
    ('inmet',        'san-01', 0, 23.4, 3.1, 7.78, 170),
    ('inmet',        'reg-01', 0, 22.9, 4.4, 5.28, 185),
    ('cemaden',      'can-01', 1, 22.1, 6.0, 8.61, 190),
    ('ciiagro',      'ite-01', 0, 23.6, 1.1, 6.11, 195)
) AS v (institute, external_code, age_h, temperature_c, precipitation_mm, wind_ms, wind_dir)
JOIN institutes i ON i.code = v.institute
JOIN stations s ON s.institute_id = i.id AND s.external_code = v.external_code
ON CONFLICT (station_id, observed_at) DO NOTHING;

COMMIT;
