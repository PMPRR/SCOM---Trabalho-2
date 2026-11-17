package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

var (
	ana   = domain.User{ID: 1, Name: "Ana", Role: domain.RoleOperator}
	bruno = domain.User{ID: 2, Name: "Bruno", Role: domain.RoleOperator}
	admin = domain.User{ID: 3, Name: "Admin", Role: domain.RoleAdmin}
)

func ownedBy(id int64) *int64 { return &id }

// managedStations: 10 is Ana's, 20 is Bruno's (turned off), 30 is official.
func managedStations() *fakeRepo {
	return &fakeRepo{stations: []domain.Station{
		{ID: 10, Name: "Sítio da Ana", InstituteCode: domain.OperatorInstitute, OwnerID: ownedBy(ana.ID), Active: true},
		{ID: 20, Name: "Chácara do Bruno", InstituteCode: domain.OperatorInstitute, OwnerID: ownedBy(bruno.ID), Active: false},
		{ID: 30, Name: "Pluviômetro oficial", InstituteCode: "cemaden", Active: true},
	}}
}

func validInput() domain.StationInput {
	return domain.StationInput{ExternalCode: "sitio-01", Name: "Sítio", Lat: -23.55, Lon: -46.63, Active: true}
}

func TestCreateMakesTheActorTheOwner(t *testing.T) {
	repo := managedStations()
	in := validInput()
	in.Name, in.ExternalCode = "  Sítio novo  ", " novo-1 "

	st, err := newService(repo).Create(context.Background(), bruno, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if st.OwnerID == nil || *st.OwnerID != bruno.ID {
		t.Errorf("owner = %v, want %d", st.OwnerID, bruno.ID)
	}
	if st.Name != "Sítio novo" || st.ExternalCode != "novo-1" {
		t.Errorf("name/code not trimmed: %q / %q", st.Name, st.ExternalCode)
	}
}

func TestCreateValidatesEveryField(t *testing.T) {
	nan := float32(math.NaN())
	low, high := float32(-101), float32(3001)
	cases := []struct {
		name  string
		edit  func(*domain.StationInput)
		field string
	}{
		{"empty name", func(in *domain.StationInput) { in.Name = "   " }, "name"},
		{"name too long", func(in *domain.StationInput) { in.Name = strings.Repeat("a", 121) }, "name"},
		{"control character", func(in *domain.StationInput) { in.Name = "Sítio\x00" }, "name"},
		{"empty code", func(in *domain.StationInput) { in.ExternalCode = "" }, "external_code"},
		{"code with space", func(in *domain.StationInput) { in.ExternalCode = "sitio 01" }, "external_code"},
		{"code with quote", func(in *domain.StationInput) { in.ExternalCode = "x'; DROP TABLE stations; --" }, "external_code"},
		{"code too long", func(in *domain.StationInput) { in.ExternalCode = strings.Repeat("a", 51) }, "external_code"},
		{"latitude north of the State", func(in *domain.StationInput) { in.Lat = -10 }, "lat"},
		{"latitude NaN", func(in *domain.StationInput) { in.Lat = math.NaN() }, "lat"},
		{"longitude east of the State", func(in *domain.StationInput) { in.Lon = -40 }, "lon"},
		{"altitude too low", func(in *domain.StationInput) { in.AltitudeM = &low }, "altitude_m"},
		{"altitude too high", func(in *domain.StationInput) { in.AltitudeM = &high }, "altitude_m"},
		{"altitude NaN", func(in *domain.StationInput) { in.AltitudeM = &nan }, "altitude_m"},
	}
	for _, c := range cases {
		repo := managedStations()
		in := validInput()
		c.edit(&in)
		_, err := newService(repo).Create(context.Background(), ana, in)

		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || invalid.Field != c.field {
			t.Errorf("%s: err = %v, want ValidationError on %q", c.name, err, c.field)
		}
		if len(repo.stations) != 3 {
			t.Errorf("%s: the invalid station was stored", c.name)
		}
	}
}

func TestAnonymousActorIsRefused(t *testing.T) {
	svc := newService(managedStations())
	if _, err := svc.Create(context.Background(), domain.User{}, validInput()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("Create: err = %v, want ErrUnauthenticated", err)
	}
	if _, _, err := svc.ListManaged(context.Background(), domain.User{}, "", 10, 0); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("ListManaged: err = %v, want ErrUnauthenticated", err)
	}
}

func TestOperatorCannotTouchSomeoneElsesStation(t *testing.T) {
	repo := managedStations()
	svc := newService(repo)
	ctx := context.Background()

	if _, err := svc.Update(ctx, ana, 20, validInput()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Update: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.SetActive(ctx, ana, 20, true); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("SetActive: err = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, ana, 20); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Delete: err = %v, want ErrForbidden", err)
	}
	if st := repo.find(20); st == nil || st.Name != "Chácara do Bruno" || st.Active {
		t.Errorf("Bruno's station changed: %+v", st)
	}
}

func TestOwnerManagesOwnStation(t *testing.T) {
	repo := managedStations()
	svc := newService(repo)
	ctx := context.Background()

	in := validInput()
	in.Name = "Sítio renomeado"
	if st, err := svc.Update(ctx, ana, 10, in); err != nil || st.Name != "Sítio renomeado" {
		t.Fatalf("Update: %+v, %v", st, err)
	}
	if st, err := svc.SetActive(ctx, ana, 10, false); err != nil || st.Active {
		t.Fatalf("SetActive: %+v, %v", st, err)
	}
	if err := svc.Delete(ctx, ana, 10); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repo.find(10) != nil {
		t.Error("station 10 still exists")
	}
}

func TestOfficialStationCanOnlyBeTurnedOffByAnAdmin(t *testing.T) {
	repo := managedStations()
	svc := newService(repo)
	ctx := context.Background()

	if _, err := svc.Update(ctx, admin, 30, validInput()); !errors.Is(err, domain.ErrOfficialStation) {
		t.Errorf("admin Update: err = %v, want ErrOfficialStation", err)
	}
	if err := svc.Delete(ctx, admin, 30); !errors.Is(err, domain.ErrOfficialStation) {
		t.Errorf("admin Delete: err = %v, want ErrOfficialStation", err)
	}
	if _, err := svc.SetActive(ctx, ana, 30, false); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("operator SetActive: err = %v, want ErrForbidden", err)
	}
	if st, err := svc.SetActive(ctx, admin, 30, false); err != nil || st.Active {
		t.Errorf("admin SetActive: %+v, %v", st, err)
	}
}

func TestAdminManagesStationsOfOperators(t *testing.T) {
	svc := newService(managedStations())
	if err := svc.Delete(context.Background(), admin, 20); err != nil {
		t.Errorf("admin Delete of an operator's station: %v", err)
	}
}

func TestListManagedShowsWhatEachUserManages(t *testing.T) {
	ctx := context.Background()
	names := func(stations []domain.Station) []string {
		out := make([]string, 0, len(stations))
		for _, st := range stations {
			out = append(out, st.Name)
		}
		return out
	}

	mine, _, err := newService(managedStations()).ListManaged(ctx, bruno, "", 50, 0)
	if err != nil || len(mine) != 1 || mine[0].ID != 20 {
		t.Errorf("Bruno sees %v (%v), want only his station, turned off as it is", names(mine), err)
	}
	all, page, err := newService(managedStations()).ListManaged(ctx, admin, "", 50, 0)
	if err != nil || page.Total != 3 {
		t.Errorf("admin sees %v (%v), want all 3", names(all), err)
	}
	found, _, _ := newService(managedStations()).ListManaged(ctx, admin, "  oficial ", 50, 0)
	if len(found) != 1 || found[0].ID != 30 {
		t.Errorf("search found %v, want the official station", names(found))
	}
	_, _, err = newService(managedStations()).ListManaged(ctx, admin, strings.Repeat("a", 121), 50, 0)
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "q" {
		t.Errorf("long search: err = %v, want ValidationError on q", err)
	}
}

func TestPublicListingHidesStationsTurnedOff(t *testing.T) {
	svc := newService(managedStations())
	stations, page, err := svc.List(context.Background(), 50, 0)
	if err != nil || page.Total != 2 {
		t.Fatalf("List: %d stations, %v; want 2", page.Total, err)
	}
	for _, st := range stations {
		if st.ID == 20 {
			t.Error("the station turned off is listed")
		}
	}
	if _, err := svc.Get(context.Background(), 20); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get of a station turned off: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetManaged(context.Background(), bruno, 20); err != nil {
		t.Errorf("its owner still reaches it: %v", err)
	}
}
