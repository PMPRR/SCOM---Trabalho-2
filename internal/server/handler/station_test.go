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

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// fakeRepo stands in for the database, so these tests exercise the real
// handler and the real service together.
type fakeRepo struct {
	stations []domain.Station
	err      error
}

func (f *fakeRepo) List(_ context.Context, limit, offset int) ([]domain.Station, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	page := make([]domain.Station, 0)
	for i := offset; i < len(f.stations) && len(page) < limit; i++ {
		page = append(page, f.stations[i])
	}
	return page, len(f.stations), nil
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

func threeStations() *fakeRepo {
	return &fakeRepo{stations: []domain.Station{
		{ID: 1, Name: "Jales"}, {ID: 2, Name: "Santos"}, {ID: 3, Name: "Campinas"},
	}}
}

func newTestServer(repo *fakeRepo, dev bool) http.Handler {
	rd := render.New(slog.New(slog.NewTextHandler(io.Discard, nil)), dev)
	h := NewStationHandler(service.NewStationService(repo), rd)

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

