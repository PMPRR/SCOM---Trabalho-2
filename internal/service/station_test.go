package service

import (
	"context"
	"errors"
	"testing"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// fakeRepo is an in-memory StationRepository.
type fakeRepo struct {
	stations []domain.Station
	calls    int
}

func (f *fakeRepo) List(_ context.Context, limit, offset int) ([]domain.Station, int, error) {
	f.calls++
	page := make([]domain.Station, 0)
	for i := offset; i < len(f.stations) && len(page) < limit; i++ {
		page = append(page, f.stations[i])
	}
	return page, len(f.stations), nil
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

func threeStations() *fakeRepo {
	return &fakeRepo{stations: []domain.Station{
		{ID: 1, Name: "Jales"}, {ID: 2, Name: "Santos"}, {ID: 3, Name: "Campinas"},
	}}
}

func TestListReturnsRequestedPage(t *testing.T) {
	svc := NewStationService(threeStations())

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
		_, _, err := NewStationService(repo).List(context.Background(), c.limit, c.offset)

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
	if _, _, err := NewStationService(threeStations()).List(context.Background(), MaxPageLimit, 0); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetReturnsStation(t *testing.T) {
	st, err := NewStationService(threeStations()).Get(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Name != "Santos" {
		t.Errorf("name = %q, want Santos", st.Name)
	}
}

func TestGetUnknownStationIsNotFound(t *testing.T) {
	_, err := NewStationService(threeStations()).Get(context.Background(), 99)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetRejectsNonPositiveID(t *testing.T) {
	for _, id := range []int64{0, -5} {
		_, err := NewStationService(threeStations()).Get(context.Background(), id)
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("id %d: err = %v, want ValidationError", id, err)
		}
	}
}
