package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// fakeRepo stands in for the database, so these tests exercise the real
// handler and the real service together. It applies filters the way the
// SQL of the repository does.
type fakeRepo struct {
	stations []domain.Station
	err      error
	readings map[int64][]domain.Observation // submissions stored, by station
}

func (f *fakeRepo) List(_ context.Context, filter domain.StationFilter, limit, offset int) ([]domain.Station, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	matched := make([]domain.Station, 0)
	for _, st := range f.stations {
		if stationMatches(st, filter) {
			matched = append(matched, st)
		}
	}
	if filter.NewestFirst {
		for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
			matched[i], matched[j] = matched[j], matched[i]
		}
	}
	page := make([]domain.Station, 0)
	for i := offset; i < len(matched) && len(page) < limit; i++ {
		page = append(page, matched[i])
	}
	return page, len(matched), nil
}

func stationMatches(st domain.Station, filter domain.StationFilter) bool {
	if filter.ActiveOnly && !st.Active {
		return false
	}
	if filter.OwnerID != nil && (st.OwnerID == nil || *st.OwnerID != *filter.OwnerID) {
		return false
	}
	if q := strings.ToLower(filter.Search); q != "" &&
		!strings.Contains(strings.ToLower(st.Name), q) && !strings.Contains(strings.ToLower(st.ExternalCode), q) {
		return false
	}
	return true
}

func (f *fakeRepo) Get(_ context.Context, id int64) (domain.Station, error) {
	if f.err != nil {
		return domain.Station{}, f.err
	}
	for _, st := range f.stations {
		if st.ID == id {
			return st, nil
		}
	}
	return domain.Station{}, domain.ErrNotFound
}

func (f *fakeRepo) Create(_ context.Context, ownerID int64, in domain.StationInput) (int64, error) {
	var id int64
	for _, st := range f.stations {
		if st.InstituteCode == domain.OperatorInstitute && st.ExternalCode == in.ExternalCode {
			return 0, domain.ErrStationCodeTaken
		}
		if in.KeyFingerprint != "" && st.KeyFingerprint == in.KeyFingerprint {
			return 0, domain.ErrKeyTaken
		}
		id = max(id, st.ID)
	}
	id++
	owner := ownerID
	st := domain.Station{
		ID: id, InstituteCode: domain.OperatorInstitute, InstituteName: "Estação particular",
		ExternalCode: in.ExternalCode, Name: in.Name, Lat: in.Lat, Lon: in.Lon,
		AltitudeM: in.AltitudeM, Active: in.Active, OwnerID: &owner,
	}
	setUplink(&st, in)
	f.stations = append(f.stations, st)
	return id, nil
}

func (f *fakeRepo) find(id int64) *domain.Station {
	for i := range f.stations {
		if f.stations[i].ID == id {
			return &f.stations[i]
		}
	}
	return nil
}

func (f *fakeRepo) Update(_ context.Context, id int64, in domain.StationInput) error {
	st := f.find(id)
	if st == nil {
		return domain.ErrNotFound
	}
	for _, other := range f.stations {
		if other.ID != id && in.KeyFingerprint != "" && other.KeyFingerprint == in.KeyFingerprint {
			return domain.ErrKeyTaken
		}
	}
	if st.KeyFingerprint != in.KeyFingerprint {
		st.LastReportAt = nil // a new key starts a new count
	}
	st.ExternalCode, st.Name, st.Lat, st.Lon = in.ExternalCode, in.Name, in.Lat, in.Lon
	st.AltitudeM, st.Active = in.AltitudeM, in.Active
	setUplink(st, in)
	return nil
}

func (f *fakeRepo) SetActive(_ context.Context, id int64, active bool) error {
	st := f.find(id)
	if st == nil {
		return domain.ErrNotFound
	}
	st.Active = active
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	for i, st := range f.stations {
		if st.ID == id {
			f.stations = append(f.stations[:i], f.stations[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func setUplink(st *domain.Station, in domain.StationInput) {
	st.PublicKey, st.KeyFingerprint, st.ReportIntervalS = in.PublicKey, in.KeyFingerprint, nil
	if in.ReportIntervalS != 0 {
		interval := in.ReportIntervalS
		st.ReportIntervalS = &interval
	}
}

// SaveSubmission follows the same rule as the SQL of the repository.
func (f *fakeRepo) SaveSubmission(_ context.Context, id int64, obs domain.Observation, receivedAt, notBefore time.Time) (bool, error) {
	st := f.find(id)
	if st == nil {
		return false, domain.ErrNotFound
	}
	for _, stored := range f.readings[id] {
		if stored.ObservedAt.Equal(obs.ObservedAt) {
			return false, nil
		}
	}
	if st.LastReportAt != nil && st.LastReportAt.After(notBefore) {
		return false, &domain.TooSoonError{Wait: st.LastReportAt.Sub(notBefore)}
	}
	if f.readings == nil {
		f.readings = map[int64][]domain.Observation{}
	}
	f.readings[id] = append(f.readings[id], obs)
	last := receivedAt
	st.LastReportAt = &last
	ob := obs
	st.Latest = &ob
	return true, nil
}

func threeStations() *fakeRepo {
	return &fakeRepo{stations: []domain.Station{
		{ID: 1, Name: "Jales", Active: true}, {ID: 2, Name: "Santos", Active: true}, {ID: 3, Name: "Campinas", Active: true},
	}}
}

func newTestServer(repo *fakeRepo, dev bool) http.Handler {
	rd := render.New(slog.New(slog.NewTextHandler(io.Discard, nil)), dev)
	h := NewStationHandler(service.NewStationService(repo, slog.New(slog.NewTextHandler(io.Discard, nil))), rd)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stations", h.List)
	mux.HandleFunc("GET /api/stations/{id}", h.Get)
	return mux
}

func get(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestListPaginates(t *testing.T) {
	rec := get(t, newTestServer(threeStations(), false), "/api/stations?limit=2&offset=1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var body struct {
		Data       []domain.Station `json:"data"`
		Pagination service.Page     `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Data) != 2 || body.Data[0].ID != 2 {
		t.Errorf("data = %+v, want stations 2 and 3", body.Data)
	}
	if body.Pagination != (service.Page{Limit: 2, Offset: 1, Total: 3}) {
		t.Errorf("pagination = %+v", body.Pagination)
	}
}

func TestListWithoutStationsIsEmptyArray(t *testing.T) {
	rec := get(t, newTestServer(&fakeRepo{}, false), "/api/stations")

	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("body = %s, want an empty array, not null", rec.Body.String())
	}
}

func TestListRejectsInvalidPagination(t *testing.T) {
	queries := []string{
		"limit=0", "limit=501", "limit=abc", "offset=-1", "offset=1.5",
		"limit=1%3BDROP%20TABLE%20stations",
	}
	for _, query := range queries {
		rec := get(t, newTestServer(threeStations(), false), "/api/stations?"+query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"invalid_parameter"`) {
			t.Errorf("%s: body = %s", query, rec.Body.String())
		}
	}
}

func TestGetReturnsStation(t *testing.T) {
	rec := get(t, newTestServer(threeStations(), false), "/api/stations/2")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"name":"Santos"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestGetUnknownStationIs404(t *testing.T) {
	rec := get(t, newTestServer(threeStations(), false), "/api/stations/99")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestGetRejectsInvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-5", "1%20OR%201=1"} {
		rec := get(t, newTestServer(threeStations(), false), "/api/stations/"+id)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q: status = %d, want 400", id, rec.Code)
		}
	}
}

func TestInternalErrorHidesCauseOutsideDev(t *testing.T) {
	repo := &fakeRepo{err: errors.New("connection refused to 10.0.0.5")}

	prod := get(t, newTestServer(repo, false), "/api/stations")
	if prod.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", prod.Code)
	}
	if strings.Contains(prod.Body.String(), "connection refused") {
		t.Errorf("production response leaks the cause: %s", prod.Body.String())
	}

	dev := get(t, newTestServer(repo, true), "/api/stations")
	if !strings.Contains(dev.Body.String(), "connection refused") {
		t.Errorf("development response should carry the cause: %s", dev.Body.String())
	}
}
