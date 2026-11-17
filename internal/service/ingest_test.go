package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

var testNow = time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)

type fakeSource struct {
	code     string
	readings []domain.Reading
	err      error
}

func (f *fakeSource) Code() string { return f.code }
func (f *fakeSource) Fetch(context.Context) ([]domain.Reading, error) {
	return f.readings, f.err
}

// fakeReadingRepo records what reached the storage layer.
type fakeReadingRepo struct {
	saved []domain.Reading
	err   error
}

func (f *fakeReadingRepo) SaveReadings(_ context.Context, readings []domain.Reading) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.saved = append(f.saved, readings...)
	return len(readings), nil
}

func f32(v float32) *float32 { return &v }
func i16(v int16) *int16     { return &v }

// goodReading is a valid reading in the city of São Paulo, one hour old.
func goodReading(code string) domain.Reading {
	return domain.Reading{
		ExternalCode: code, StationName: "Estação " + code, Lat: -23.55, Lon: -46.63,
		Observation: domain.Observation{
			ObservedAt: testNow.Add(-time.Hour), TemperatureC: f32(22.5), WindSpeedMS: f32(3), WindDirDeg: i16(150),
		},
	}
}

func newTestIngest(repo *fakeReadingRepo, sources ...Source) *IngestService {
	svc := NewIngestService(sources, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.now = func() time.Time { return testNow }
	return svc
}

func TestIngestSavesValidReadings(t *testing.T) {
	repo := &fakeReadingRepo{}
	svc := newTestIngest(repo, &fakeSource{code: "inmet", readings: []domain.Reading{goodReading("A701"), goodReading("A702")}})

	results := svc.RunOnce(context.Background())

	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	if r := results[0]; r.Fetched != 2 || r.Valid != 2 || r.Saved != 2 {
		t.Errorf("result = %+v, want 2 fetched, valid and saved", r)
	}
	if len(repo.saved) != 2 || repo.saved[0].InstituteCode != "inmet" {
		t.Errorf("saved = %+v", repo.saved)
	}
}

func TestIngestRejectsUntrustworthyReadings(t *testing.T) {
	outside := goodReading("RIO")
	outside.Lat, outside.Lon = -22.9, -43.2 // Rio de Janeiro

	future := goodReading("FUT")
	future.Observation.ObservedAt = testNow.Add(2 * time.Hour)

	stale := goodReading("OLD")
	stale.Observation.ObservedAt = testNow.Add(-72 * time.Hour)

	noCode := goodReading("")

	empty := goodReading("EMPTY")
	empty.Observation = domain.Observation{ObservedAt: testNow.Add(-time.Hour)}

	repo := &fakeReadingRepo{}
	svc := newTestIngest(repo, &fakeSource{code: "inmet", readings: []domain.Reading{
		outside, future, stale, noCode, empty, goodReading("OK"),
	}})

	result := svc.RunOnce(context.Background())[0]

	if result.Fetched != 6 || result.Valid != 1 {
		t.Errorf("result = %+v, want 6 fetched and 1 valid", result)
	}
	if len(repo.saved) != 1 || repo.saved[0].ExternalCode != "OK" {
		t.Errorf("saved = %+v, want only OK", repo.saved)
	}
}

func TestIngestDropsImpossibleValuesButKeepsTheRest(t *testing.T) {
	reading := goodReading("A701")
	reading.Observation.TemperatureC = f32(22.5)
	reading.Observation.HumidityPct = f32(140)   // above 100 %
	reading.Observation.PressureHPa = f32(9999)  // failure code of a source
	reading.Observation.WindSpeedMS = f32(-1)    // negative speed
	reading.Observation.WindDirDeg = i16(400)    // beyond a full turn
	reading.Observation.PrecipitationMM = f32(0) // zero is a real value

	repo := &fakeReadingRepo{}
	newTestIngest(repo, &fakeSource{code: "inmet", readings: []domain.Reading{reading}}).RunOnce(context.Background())

	if len(repo.saved) != 1 {
		t.Fatalf("saved %d readings, want 1", len(repo.saved))
	}
	o := repo.saved[0].Observation
	if o.TemperatureC == nil || *o.TemperatureC != 22.5 {
		t.Errorf("temperature = %v, want 22.5", o.TemperatureC)
	}
	if o.PrecipitationMM == nil || *o.PrecipitationMM != 0 {
		t.Errorf("precipitation = %v, want 0", o.PrecipitationMM)
	}
	if o.HumidityPct != nil || o.PressureHPa != nil || o.WindSpeedMS != nil || o.WindDirDeg != nil {
		t.Errorf("impossible values were kept: %+v", o)
	}
}

func TestIngestSourceCannotWriteUnderAnotherInstitute(t *testing.T) {
	reading := goodReading("X1")
	reading.InstituteCode = "inmet" // a model posing as the official network

	repo := &fakeReadingRepo{}
	newTestIngest(repo, &fakeSource{code: "openmeteo", readings: []domain.Reading{reading}}).RunOnce(context.Background())

	if len(repo.saved) != 1 || repo.saved[0].InstituteCode != "openmeteo" {
		t.Errorf("saved = %+v, want institute openmeteo", repo.saved)
	}
}

func TestIngestOneSourceFailingDoesNotStopTheOthers(t *testing.T) {
	repo := &fakeReadingRepo{}
	svc := newTestIngest(repo,
		&fakeSource{code: "inmet", err: errors.New("unexpected status 503")},
		&fakeSource{code: "metar", readings: []domain.Reading{goodReading("SBGR")}},
	)

	results := svc.RunOnce(context.Background())

	if results[0].Err == nil || results[0].Saved != 0 {
		t.Errorf("first source = %+v, want an error", results[0])
	}
	if results[1].Err != nil || results[1].Saved != 1 {
		t.Errorf("second source = %+v, want 1 saved", results[1])
	}
}

func TestIngestReportsStorageFailure(t *testing.T) {
	repo := &fakeReadingRepo{err: errors.New(`institute "metar" is not registered`)}
	svc := newTestIngest(repo, &fakeSource{code: "metar", readings: []domain.Reading{goodReading("SBGR")}})

	if result := svc.RunOnce(context.Background())[0]; result.Err == nil {
		t.Errorf("result = %+v, want the storage error", result)
	}
}

func TestIngestTruncatesLongStationNames(t *testing.T) {
	reading := goodReading("A701")
	for len([]rune(reading.StationName)) <= maxStationNameLength {
		reading.StationName += " São Paulo"
	}

	repo := &fakeReadingRepo{}
	newTestIngest(repo, &fakeSource{code: "inmet", readings: []domain.Reading{reading}}).RunOnce(context.Background())

	if n := len([]rune(repo.saved[0].StationName)); n != maxStationNameLength {
		t.Errorf("name has %d characters, want %d", n, maxStationNameLength)
	}
}
