-- +goose Up
CREATE TABLE observations (
    station_id       BIGINT      NOT NULL REFERENCES stations (id) ON DELETE CASCADE,
    observed_at      TIMESTAMPTZ NOT NULL,
    temperature_c    REAL     CHECK (temperature_c BETWEEN -90 AND 60),
    humidity_pct     REAL     CHECK (humidity_pct BETWEEN 0 AND 100),
    precipitation_mm REAL     CHECK (precipitation_mm >= 0),
    wind_speed_ms    REAL     CHECK (wind_speed_ms >= 0),
    wind_dir_deg     SMALLINT CHECK (wind_dir_deg BETWEEN 0 AND 360),
    pressure_hpa     REAL     CHECK (pressure_hpa BETWEEN 800 AND 1100),
    ingested_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (station_id, observed_at)
);

CREATE INDEX observations_observed_at_idx ON observations (observed_at);

-- +goose Down
DROP TABLE observations;
