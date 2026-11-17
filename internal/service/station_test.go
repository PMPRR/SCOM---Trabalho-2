package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// fakeRepo is an in-memory StationRepository. It applies filters the way
// the SQL of the repository does, and records the writes it receives.
type fakeRepo struct {
	stations []domain.Station
	calls    int
	filters  []domain.StationFilter
}

func (f *fakeRepo) List(_ context.Context, filter domain.StationFilter, limit, offset int) ([]domain.Station, int, error) {
	f.calls++
	f.filters = append(f.filters, filter)
	matched := make([]domain.Station, 0)
	for _, st := range f.stations {
		if filter.ActiveOnly && !st.Active {
			continue
		}
		if filter.OwnerID != nil && (st.OwnerID == nil || *st.OwnerID != *filter.OwnerID) {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(st.Name), strings.ToLower(filter.Search)) {
			continue
		}
		matched = append(matched, st)
	}
	page := make([]domain.Station, 0)
	for i := offset; i < len(matched) && len(page) < limit; i++ {
		page = append(page, matched[i])
	}
	return page, len(matched), nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (domain.Station, error) {
	f.calls++
	for _, st := range f.stations {
		if st.ID == id {
			return st, nil
		}
	}
	return domain.Station{}, domain.ErrNotFound
}

func (f *fakeRepo) find(id int64) *domain.Station {
	for i := range f.stations {
		if f.stations[i].ID == id {
			return &f.stations[i]
		}
	}
	return nil
}

func (f *fakeRepo) Create(_ context.Context, ownerID int64, in domain.StationInput) (int64, error) {
	f.calls++
	id := int64(len(f.stations) + 100)
	owner := ownerID
	f.stations = append(f.stations, domain.Station{
		ID: id, InstituteCode: domain.OperatorInstitute, ExternalCode: in.ExternalCode, Name: in.Name,
		Lat: in.Lat, Lon: in.Lon, AltitudeM: in.AltitudeM, Active: in.Active, OwnerID: &owner,
	})
	return id, nil
}

func (f *fakeRepo) Update(_ context.Context, id int64, in domain.StationInput) error {
	f.calls++
	st := f.find(id)
	if st == nil {
		return domain.ErrNotFound
	}
	st.ExternalCode, st.Name, st.Lat, st.Lon, st.AltitudeM, st.Active = in.ExternalCode, in.Name, in.Lat, in.Lon, in.AltitudeM, in.Active
	return nil
}

func (f *fakeRepo) SetActive(_ context.Context, id int64, active bool) error {
	f.calls++
	st := f.find(id)
	if st == nil {
		return domain.ErrNotFound
	}
	st.Active = active
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	f.calls++
	for i, st := range f.stations {
		if st.ID == id {
			f.stations = append(f.stations[:i], f.stations[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func threeStations() *fakeRepo {
	return &fakeRepo{stations: []domain.Station{
		{ID: 1, Name: "Jales", Active: true}, {ID: 2, Name: "Santos", Active: true}, {ID: 3, Name: "Campinas", Active: true},
	}}
}

func TestListReturnsRequestedPage(t *testing.T) {
	svc := newService(threeStations())

	stations, page, err := svc.List(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stations) != 2 || stations[0].ID != 2 {
		t.Errorf("stations = %+v, want 2 and 3", stations)
	}
	if page != (Page{Limit: 2, Offset: 1, Total: 3}) {
		t.Errorf("page = %+v", page)
	}
}

func TestListRejectsOutOfRangePagination(t *testing.T) {
	cases := []struct {
		name          string
		limit, offset int
		field         string
	}{
		{"zero limit", 0, 0, "limit"},
		{"negative limit", -1, 0, "limit"},
		{"limit above maximum", MaxPageLimit + 1, 0, "limit"},
		{"negative offset", 10, -1, "offset"},
	}
	for _, c := range cases {
		repo := threeStations()
		_, _, err := newService(repo).List(context.Background(), c.limit, c.offset)

		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want ValidationError", c.name, err)
			continue
		}
		if invalid.Field != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, invalid.Field, c.field)
		}
		if repo.calls != 0 {
			t.Errorf("%s: invalid input reached the database", c.name)
		}
	}
}

func TestListAcceptsMaximumLimit(t *testing.T) {
	if _, _, err := newService(threeStations()).List(context.Background(), MaxPageLimit, 0); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetReturnsStation(t *testing.T) {
	st, err := newService(threeStations()).Get(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Name != "Santos" {
		t.Errorf("name = %q, want Santos", st.Name)
	}
}

func TestGetUnknownStationIsNotFound(t *testing.T) {
	_, err := newService(threeStations()).Get(context.Background(), 99)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetRejectsNonPositiveID(t *testing.T) {
	for _, id := range []int64{0, -5} {
		_, err := newService(threeStations()).Get(context.Background(), id)
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("id %d: err = %v, want ValidationError", id, err)
		}
	}
}

func TestConfidenceRanksSourcesAndAge(t *testing.T) {
	now := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)
	owner := int64(7)
	fresh := &domain.Observation{ObservedAt: now.Add(-30 * time.Minute)}
	stale := &domain.Observation{ObservedAt: now.Add(-3 * time.Hour)}

	cases := []struct {
		name    string
		station domain.Station
		want    domain.Confidence
	}{
		{"official station, fresh reading", domain.Station{InstituteCode: "inmet", Latest: fresh}, domain.ConfidenceHigh},
		{"private station, fresh reading", domain.Station{InstituteCode: "privada", OwnerID: &owner, Latest: fresh}, domain.ConfidenceMedium},
		{"model point", domain.Station{InstituteCode: "openmeteo", Latest: fresh}, domain.ConfidenceLow},
		{"official station, stale reading", domain.Station{InstituteCode: "inmet", Latest: stale}, domain.ConfidenceLow},
		{"station without reading", domain.Station{InstituteCode: "inmet"}, ""},
	}
	for _, c := range cases {
		if got := confidenceOf(c.station, now); got != c.want {
			t.Errorf("%s: confidence = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestListAndGetFillConfidence(t *testing.T) {
	now := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)
	repo := &fakeRepo{stations: []domain.Station{
		{ID: 1, InstituteCode: "inmet", Active: true, Latest: &domain.Observation{ObservedAt: now.Add(-time.Hour)}},
	}}
	svc := newService(repo)
	svc.now = func() time.Time { return now }

	stations, _, err := svc.List(context.Background(), 10, 0)
	if err != nil || stations[0].Confidence != domain.ConfidenceHigh {
		t.Errorf("List: confidence = %q, err = %v", stations[0].Confidence, err)
	}
	station, err := svc.Get(context.Background(), 1)
	if err != nil || station.Confidence != domain.ConfidenceHigh {
		t.Errorf("Get: confidence = %q, err = %v", station.Confidence, err)
	}
}

func newService(repo StationRepository) *StationService {
	return NewStationService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
