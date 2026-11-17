package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// Bounding box of the State of São Paulo, with a small margin. A reading
// outside it is a source reporting the wrong place.
const (
	minLat, maxLat = -25.5, -19.5
	minLon, maxLon = -53.5, -44.0
)

// A reading older than this is history, not current weather; one dated in
// the future (beyond clock drift) is a source with a broken clock.
const (
	maxReadingAge   = 48 * time.Hour
	maxClockAdvance = 15 * time.Minute
)

const maxStationNameLength = 120 // the limit of stations.name

// Source is an external service that reports current observations.
type Source interface {
	// Code is the institutes.code the readings belong to.
	Code() string
	Fetch(ctx context.Context) ([]domain.Reading, error)
}

// ReadingRepository stores readings, creating the stations it has not seen.
// It returns how many observations were new.
type ReadingRepository interface {
	SaveReadings(ctx context.Context, readings []domain.Reading) (int, error)
}

// IngestResult summarizes one source in one run.
type IngestResult struct {
	Source  string
	Fetched int // readings the source returned
	Valid   int // readings that passed validation
	Saved   int // observations that were not in the database yet
	Err     error
}

// IngestService collects readings from every source, validates them and
// stores them. Sources are independent: one failing does not stop the rest.
type IngestService struct {
	sources []Source
	repo    ReadingRepository
	log     *slog.Logger
	now     func() time.Time
}

func NewIngestService(sources []Source, repo ReadingRepository, log *slog.Logger) *IngestService {
	return &IngestService{sources: sources, repo: repo, log: log, now: time.Now}
}

// RunOnce asks every source once.
func (s *IngestService) RunOnce(ctx context.Context) []IngestResult {
	results := make([]IngestResult, 0, len(s.sources))
	for _, source := range s.sources {
		result := s.collect(ctx, source)
		if result.Err != nil {
			s.log.Error("ingest failed", "source", result.Source, "error", result.Err)
		} else {
			s.log.Info("ingest finished", "source", result.Source,
				"fetched", result.Fetched, "valid", result.Valid, "saved", result.Saved)
		}
		results = append(results, result)
	}
	return results
}

// Run collects immediately and then at every interval, until ctx ends.
func (s *IngestService) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *IngestService) collect(ctx context.Context, source Source) IngestResult {
	result := IngestResult{Source: source.Code()}

	readings, err := source.Fetch(ctx)
	if err != nil {
		result.Err = err
		return result
	}
	result.Fetched = len(readings)

	now := s.now()
	valid := make([]domain.Reading, 0, len(readings))
	for _, reading := range readings {
		// A source may only write under its own institute.
		reading.InstituteCode = source.Code()
		if clean, ok := normalizeReading(reading, now); ok {
			valid = append(valid, clean)
		}
	}
	result.Valid = len(valid)
	if len(valid) == 0 {
		return result
	}

	result.Saved, result.Err = s.repo.SaveReadings(ctx, valid)
	return result
}

// normalizeReading applies the rules every external reading must satisfy
// before it reaches the database. It rejects the reading when the station or
// the time cannot be trusted, and discards individual values that are
// physically impossible, keeping the rest. The limits are the same as the
// CHECK constraints of the observations table, so a bad value is dropped
// here instead of aborting the whole batch there.
func normalizeReading(r domain.Reading, now time.Time) (domain.Reading, bool) {
	r.ExternalCode = strings.TrimSpace(r.ExternalCode)
	r.StationName = strings.TrimSpace(r.StationName)
	if r.ExternalCode == "" || len(r.ExternalCode) > 50 {
		return r, false
	}
	if r.StationName == "" {
		r.StationName = r.ExternalCode
	}
	if runes := []rune(r.StationName); len(runes) > maxStationNameLength {
		r.StationName = string(runes[:maxStationNameLength])
	}
	if r.Lat < minLat || r.Lat > maxLat || r.Lon < minLon || r.Lon > maxLon {
		return r, false
	}

	o := &r.Observation
	if o.ObservedAt.IsZero() || o.ObservedAt.After(now.Add(maxClockAdvance)) || o.ObservedAt.Before(now.Add(-maxReadingAge)) {
		return r, false
	}
	o.ObservedAt = o.ObservedAt.UTC()

	o.TemperatureC = within(o.TemperatureC, -90, 60)
	o.HumidityPct = within(o.HumidityPct, 0, 100)
	o.PrecipitationMM = within(o.PrecipitationMM, 0, 500)
	o.WindSpeedMS = within(o.WindSpeedMS, 0, 120)
	o.PressureHPa = within(o.PressureHPa, 800, 1100)
	if o.WindDirDeg != nil && (*o.WindDirDeg < 0 || *o.WindDirDeg > 360) {
		o.WindDirDeg = nil
	}

	hasValue := o.TemperatureC != nil || o.HumidityPct != nil || o.PrecipitationMM != nil ||
		o.WindSpeedMS != nil || o.PressureHPa != nil
	return r, hasValue
}

// within returns v when it lies in [min, max], and nil otherwise.
func within(v *float32, min, max float32) *float32 {
	if v == nil || *v < min || *v > max || *v != *v { // v != v is true only for NaN
		return nil
	}
	return v
}
