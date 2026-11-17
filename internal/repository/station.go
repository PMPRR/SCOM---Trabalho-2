// Package repository isolates all database access. Nothing outside this
// package writes SQL.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
       s.public_key, s.key_fingerprint, s.report_interval_s, s.last_report_at,
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

// List returns one page of the stations that pass the filter, plus how many
// pass it in all, so the caller can paginate.
func (r *StationRepository) List(ctx context.Context, filter domain.StationFilter, limit, offset int) ([]domain.Station, int, error) {
	where, args := stationWhere(filter)

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM stations s`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stations: %w", err)
	}

	order := ` ORDER BY s.id`
	if filter.NewestFirst {
		order = ` ORDER BY s.id DESC`
	}
	query := stationSelect + where + order + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	rows, err := r.db.Query(ctx, query, append(args, limit, offset)...)
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

// stationWhere turns a filter into a WHERE clause and its arguments. Only
// the placeholders ($1, $2...) are written into the SQL text; every value
// typed by a user travels as an argument, so it can never become SQL.
func stationWhere(filter domain.StationFilter) (string, []any) {
	var (
		conditions []string
		args       []any
	)
	if filter.ActiveOnly {
		conditions = append(conditions, "s.active")
	}
	if filter.OwnerID != nil {
		args = append(args, *filter.OwnerID)
		conditions = append(conditions, fmt.Sprintf("s.owner_id = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+escapeLike(filter.Search)+"%")
		conditions = append(conditions, fmt.Sprintf("(s.name ILIKE $%[1]d OR s.external_code ILIKE $%[1]d)", len(args)))
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// escapeLike makes %, _ and the escape character itself match literally in
// a LIKE pattern, so a search for "50%" looks for those three characters.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
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

// insertStation creates a station of the institute named by $1. When the
// institute is not registered the SELECT yields no row and nothing is
// inserted. Longitude comes first in ST_MakePoint.
const insertStation = `
INSERT INTO stations (institute_id, external_code, name, location, altitude_m, active, owner_id,
                      public_key, key_fingerprint, report_interval_s)
SELECT i.id, $2::text, $3::text,
       ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
       $6::real, $7::boolean, $8::bigint,
       NULLIF($9::text, ''), NULLIF($10::text, ''), NULLIF($11::integer, 0)
FROM institutes i
WHERE i.code = $1
RETURNING id`

// Create stores a station registered on the site by ownerID and returns
// its id. The UNIQUE (institute_id, external_code) constraint is the
// authority on duplicate codes.
func (r *StationRepository) Create(ctx context.Context, ownerID int64, in domain.StationInput) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, insertStation,
		domain.OperatorInstitute, in.ExternalCode, in.Name, in.Lon, in.Lat, in.AltitudeM, in.Active, ownerID,
		in.PublicKey, in.KeyFingerprint, in.ReportIntervalS,
	).Scan(&id)
	switch {
	case isUniqueViolation(err):
		return 0, stationConflict(err)
	case errors.Is(err, pgx.ErrNoRows):
		return 0, fmt.Errorf("institute %q is not registered: run the migrations", domain.OperatorInstitute)
	case err != nil:
		return 0, fmt.Errorf("create station: %w", err)
	}
	return id, nil
}

// Update replaces what a user can change in a station.
func (r *StationRepository) Update(ctx context.Context, id int64, in domain.StationInput) error {
	tag, err := r.db.Exec(ctx, `
UPDATE stations
SET external_code = $2, name = $3,
    location = ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
    altitude_m = $6, active = $7,
    public_key = NULLIF($8::text, ''), key_fingerprint = NULLIF($9::text, ''),
    report_interval_s = NULLIF($10::integer, 0),
    -- A new key starts a new count: the old one may have sent minutes ago.
    last_report_at = CASE WHEN key_fingerprint IS NOT DISTINCT FROM NULLIF($9::text, '')
                          THEN last_report_at END,
    updated_at = now()
WHERE id = $1`,
		id, in.ExternalCode, in.Name, in.Lon, in.Lat, in.AltitudeM, in.Active,
		in.PublicKey, in.KeyFingerprint, in.ReportIntervalS)
	switch {
	case isUniqueViolation(err):
		return stationConflict(err)
	case err != nil:
		return fmt.Errorf("update station %d: %w", id, err)
	case tag.RowsAffected() == 0:
		return domain.ErrNotFound
	}
	return nil
}

// SetActive turns a station on or off. A station turned off keeps its
// readings but leaves the public listings and the map.
func (r *StationRepository) SetActive(ctx context.Context, id int64, active bool) error {
	tag, err := r.db.Exec(ctx, `UPDATE stations SET active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return fmt.Errorf("set station %d active: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes a station. Its observations go with it (ON DELETE CASCADE).
func (r *StationRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM stations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete station %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// stationConflict tells which UNIQUE constraint a new or changed station
// broke: the public key, or the code inside the institute.
func stationConflict(err error) error {
	if constraintOf(err) == "stations_key_fingerprint_key" {
		return domain.ErrKeyTaken
	}
	return domain.ErrStationCodeTaken
}

// SaveSubmission stores a reading sent by the station itself, in one
// transaction. It returns false, without error, when a reading with the same
// time is already stored: a station that did not get the answer and sends
// again is not refused. It returns a *domain.TooSoonError when the previous
// submission was later than notBefore.
//
// The interval is checked and the time of this submission recorded by the
// same UPDATE, which locks the row of the station: two submissions at the
// same moment cannot both pass.
func (r *StationRepository) SaveSubmission(ctx context.Context, stationID int64, obs domain.Observation, receivedAt, notBefore time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var duplicate bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM observations WHERE station_id = $1 AND observed_at = $2)`,
		stationID, obs.ObservedAt,
	).Scan(&duplicate)
	if err != nil {
		return false, fmt.Errorf("look up reading of station %d: %w", stationID, err)
	}
	if duplicate {
		return false, nil
	}

	var id int64
	err = tx.QueryRow(ctx, `
UPDATE stations SET last_report_at = $2
WHERE id = $1 AND (last_report_at IS NULL OR last_report_at <= $3)
RETURNING id`, stationID, receivedAt, notBefore).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		var last *time.Time
		if err := tx.QueryRow(ctx, `SELECT last_report_at FROM stations WHERE id = $1`, stationID).Scan(&last); err != nil {
			return false, fmt.Errorf("read last report of station %d: %w", stationID, err)
		}
		if last == nil { // the station is gone
			return false, domain.ErrNotFound
		}
		return false, &domain.TooSoonError{Wait: last.Sub(notBefore)}
	}
	if err != nil {
		return false, fmt.Errorf("record report of station %d: %w", stationID, err)
	}

	_, err = tx.Exec(ctx, insertObservation,
		stationID, obs.ObservedAt, obs.TemperatureC, obs.HumidityPct, obs.PrecipitationMM,
		obs.WindSpeedMS, obs.WindDirDeg, obs.PressureHPa,
	)
	if err != nil {
		return false, fmt.Errorf("save reading of station %d: %w", stationID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// scanStation reads one row produced by stationSelect.
func scanStation(row pgx.Row) (domain.Station, error) {
	var (
		st             domain.Station
		observedAt     *time.Time
		obs            domain.Observation
		publicKey      *string
		keyFingerprint *string
	)
	err := row.Scan(
		&st.ID, &st.InstituteCode, &st.InstituteName, &st.ExternalCode, &st.Name,
		&st.Lat, &st.Lon,
		&st.AltitudeM, &st.Active, &st.OwnerID, &st.CreatedAt, &st.UpdatedAt,
		&publicKey, &keyFingerprint, &st.ReportIntervalS, &st.LastReportAt,
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
	if publicKey != nil && keyFingerprint != nil {
		st.PublicKey, st.KeyFingerprint = *publicKey, *keyFingerprint
	}
	return st, nil
}
