package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// ReadingRepository stores readings collected from external sources.
type ReadingRepository struct {
	db *pgxpool.Pool
}

func NewReadingRepository(db *pgxpool.Pool) *ReadingRepository {
	return &ReadingRepository{db: db}
}

// upsertStation creates the station on its first reading and keeps its name
// and position in line with the source afterwards. When the institute code
// is unknown the SELECT yields no row and nothing is inserted.
const upsertStation = `
INSERT INTO stations (institute_id, external_code, name, location, altitude_m)
SELECT i.id, $2::text, $3::text,
       ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
       $6::real
FROM institutes i
WHERE i.code = $1
ON CONFLICT (institute_id, external_code) DO UPDATE
SET name       = EXCLUDED.name,
    location   = EXCLUDED.location,
    altitude_m = COALESCE(EXCLUDED.altitude_m, stations.altitude_m)
RETURNING id`

// insertObservation ignores a reading that is already stored: sources are
// asked every hour and often repeat the previous one.
const insertObservation = `
INSERT INTO observations
    (station_id, observed_at, temperature_c, humidity_pct, precipitation_mm,
     wind_speed_ms, wind_dir_deg, pressure_hpa)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (station_id, observed_at) DO NOTHING`

// SaveReadings stores a batch in one transaction, so a source is recorded
// whole or not at all. It returns how many observations were new.
func (r *ReadingRepository) SaveReadings(ctx context.Context, readings []domain.Reading) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback after a successful Commit does nothing.
	defer func() { _ = tx.Rollback(ctx) }()

	saved := 0
	for _, reading := range readings {
		var stationID int64
		err := tx.QueryRow(ctx, upsertStation,
			reading.InstituteCode, reading.ExternalCode, reading.StationName,
			reading.Lon, reading.Lat, reading.AltitudeM,
		).Scan(&stationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("institute %q is not registered", reading.InstituteCode)
		}
		if err != nil {
			return 0, fmt.Errorf("save station %s/%s: %w", reading.InstituteCode, reading.ExternalCode, err)
		}

		o := reading.Observation
		tag, err := tx.Exec(ctx, insertObservation,
			stationID, o.ObservedAt, o.TemperatureC, o.HumidityPct, o.PrecipitationMM,
			o.WindSpeedMS, o.WindDirDeg, o.PressureHPa,
		)
		if err != nil {
			return 0, fmt.Errorf("save observation of %s/%s: %w", reading.InstituteCode, reading.ExternalCode, err)
		}
		saved += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return saved, nil
}
