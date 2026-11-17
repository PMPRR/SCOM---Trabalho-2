package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// serve starts a fake API that answers each path with a fixed body.
func serve(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func byCode(readings []domain.Reading) map[string]domain.Reading {
	out := make(map[string]domain.Reading, len(readings))
	for _, r := range readings {
		out[r.ExternalCode] = r
	}
	return out
}

func near(got *float32, want float32) bool {
	return got != nil && *got > want-0.01 && *got < want+0.01
}

func TestOpenMeteoPairsAnswersWithPoints(t *testing.T) {
	srv := serve(t, map[string]string{"/v1/forecast": `[
		{"latitude":-20.25,"longitude":-50.5,"elevation":478.0,
		 "current":{"time":1794860100,"interval":900,"temperature_2m":31.2,
		            "relative_humidity_2m":40,"wind_speed_10m":2.5,
		            "wind_direction_10m":85,"surface_pressure":957.3}},
		{"latitude":-23.5,"longitude":-46.6,"elevation":760.0,
		 "current":{"time":1794860100,"interval":900,"temperature_2m":22.4,
		            "relative_humidity_2m":71,"wind_speed_10m":3.1,
		            "wind_direction_10m":150,"surface_pressure":926.0}}]`})
	source := &OpenMeteo{Client: srv.Client(), BaseURL: srv.URL, Points: []Point{
		{Code: "jales", Name: "Jales", Lat: -20.2687, Lon: -50.5456},
		{Code: "sao-paulo", Name: "São Paulo", Lat: -23.5505, Lon: -46.6333},
	}}

	readings, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("%d readings, want 2", len(readings))
	}
	sp := byCode(readings)["sao-paulo"]
	if sp.InstituteCode != "openmeteo" || sp.StationName != "São Paulo (modelo)" {
		t.Errorf("station = %+v", sp)
	}
	// The position is the requested point, not the grid cell of the model.
	if sp.Lat != -23.5505 || sp.Lon != -46.6333 {
		t.Errorf("position = %v, %v", sp.Lat, sp.Lon)
	}
	o := sp.Observation
	if !near(o.TemperatureC, 22.4) || !near(o.WindSpeedMS, 3.1) || !near(o.PressureHPa, 926) || o.WindDirDeg == nil || *o.WindDirDeg != 150 {
		t.Errorf("observation = %+v", o)
	}
	if o.PrecipitationMM != nil {
		t.Errorf("precipitation should be left out, got %v", *o.PrecipitationMM)
	}
	if !o.ObservedAt.Equal(time.Unix(1794860100, 0)) {
		t.Errorf("observed at %v", o.ObservedAt)
	}
}

func TestOpenMeteoRejectsMismatchedAnswer(t *testing.T) {
	srv := serve(t, map[string]string{"/v1/forecast": `[{"elevation":478,"current":{"time":1794860100,"temperature_2m":31.2}}]`})
	source := &OpenMeteo{Client: srv.Client(), BaseURL: srv.URL, Points: []Point{
		{Code: "a", Name: "A", Lat: -20, Lon: -50}, {Code: "b", Name: "B", Lat: -21, Lon: -49},
	}}

	if _, err := source.Fetch(context.Background()); err == nil {
		t.Error("want an error when the answer has fewer locations than requested")
	}
}

func TestErrorsDoNotCarryTheURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused

	err := getJSON(context.Background(), http.DefaultClient, srv.URL+"/token/abc123", new(any))
	if err == nil {
		t.Fatal("want a connection error")
	}
	if strings.Contains(err.Error(), "abc123") {
		t.Errorf("error leaks the URL: %v", err)
	}
}
