// Package repository isolates all database access. Nothing outside this
// package writes SQL.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// StationRepository reads stations from PostgreSQL.
type StationRepository struct {
	db *pgxpool.Pool
}

func NewStationRepository(db *pgxpool.Pool) *StationRepository {
	return &StationRepository{db: db}
}

// stationSelect joins each station with its institute and its latest
// observation. The lateral subquery uses the observations primary key
// (station_id, observed_at), so it costs one index lookup per station.
const stationSelect = `
SELECT s.id, i.code, i.name, s.external_code, s.name,
       ST_Y(s.location::geometry), ST_X(s.location::geometry),
       s.altitude_m, s.active, s.owner_id, s.created_at, s.updated_at,
       o.observed_at, o.temperature_c, o.humidity_pct, o.precipitation_mm,
       o.wind_speed_ms, o.wind_dir_deg, o.pressure_hpa
FROM stations s
JOIN institutes i ON i.id = s.institute_id
LEFT JOIN LATERAL (
    SELECT observed_at, temperature_c, humidity_pct, precipitation_mm,
           wind_speed_ms, wind_dir_deg, pressure_hpa
    FROM observations
    WHERE station_id = s.id
    ORDER BY observed_at DESC
    LIMIT 1
) o ON true`

// List returns one page of stations ordered by id, plus the total number of
// stations so the caller can paginate.
func (r *StationRepository) List(ctx context.Context, limit, offset int) ([]domain.Station, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM stations`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stations: %w", err)
	}

	rows, err := r.db.Query(ctx, stationSelect+` ORDER BY s.id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list stations: %w", err)
	}
	defer rows.Close()

	stations := make([]domain.Station, 0, limit)
	for rows.Next() {
		st, err := scanStation(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan station: %w", err)
		}
		stations = append(stations, st)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list stations: %w", err)
	}
	return stations, total, nil
}

// Get returns one station, or domain.ErrNotFound.
func (r *StationRepository) Get(ctx context.Context, id int64) (domain.Station, error) {
	st, err := scanStation(r.db.QueryRow(ctx, stationSelect+` WHERE s.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Station{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Station{}, fmt.Errorf("get station %d: %w", id, err)
	}
	return st, nil
}

// scanStation reads one row produced by stationSelect.
func scanStation(row pgx.Row) (domain.Station, error) {
	var (
		st         domain.Station
		observedAt *time.Time
		obs        domain.Observation
	)
	err := row.Scan(
		&st.ID, &st.InstituteCode, &st.InstituteName, &st.ExternalCode, &st.Name,
		&st.Lat, &st.Lon,
		&st.AltitudeM, &st.Active, &st.OwnerID, &st.CreatedAt, &st.UpdatedAt,
		&observedAt, &obs.TemperatureC, &obs.HumidityPct, &obs.PrecipitationMM,
		&obs.WindSpeedMS, &obs.WindDirDeg, &obs.PressureHPa,
	)
	if err != nil {
		return domain.Station{}, err
	}
	if observedAt != nil {
		obs.ObservedAt = *observedAt
		st.Latest = &obs
	}
	return st, nil
}

