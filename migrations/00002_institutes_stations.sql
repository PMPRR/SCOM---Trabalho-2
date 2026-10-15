-- +goose Up
CREATE TABLE institutes (
    id         SMALLINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9_]{2,20}$'),
    name       TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    website    TEXT,
    active     BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stations (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    institute_id  SMALLINT               NOT NULL REFERENCES institutes (id) ON DELETE RESTRICT,
    external_code TEXT                   NOT NULL CHECK (length(external_code) BETWEEN 1 AND 50),
    name          TEXT                   NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    location      geography(Point, 4326) NOT NULL,
    altitude_m    REAL,
    active        BOOLEAN                NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ            NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ            NOT NULL DEFAULT now(),
    UNIQUE (institute_id, external_code)
);

CREATE INDEX stations_location_gix ON stations USING GIST (location);

-- +goose Down
DROP TABLE stations;
DROP TABLE institutes;
