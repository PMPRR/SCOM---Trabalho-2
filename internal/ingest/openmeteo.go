package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// Point is a place where a gridded model is sampled.
type Point struct {
	Code string
	Name string
	Lat  float64
	Lon  float64
}

// OpenMeteo samples the Open-Meteo forecast model at fixed points. It needs
// no key. The values are model output, not measurements: they fill the gaps
// between real stations, and the map gives them the lowest confidence.
//
// Precipitation is left out on purpose. The "current" block reports the
// total of the last 15 minutes, while every other source and the map itself
// work with the total of the last hour.
type OpenMeteo struct {
	Client  *http.Client
	BaseURL string
	Points  []Point
}

func NewOpenMeteo(client *http.Client) *OpenMeteo {
	return &OpenMeteo{Client: client, BaseURL: "https://api.open-meteo.com", Points: SaoPauloPoints}
}

func (s *OpenMeteo) Code() string { return "openmeteo" }

type openMeteoLocation struct {
	Elevation number `json:"elevation"`
	Current   *struct {
		Time        int64  `json:"time"` // unix seconds (timeformat=unixtime)
		Temperature number `json:"temperature_2m"`
		Humidity    number `json:"relative_humidity_2m"`
		WindSpeed   number `json:"wind_speed_10m"` // m/s (wind_speed_unit=ms)
		WindDir     number `json:"wind_direction_10m"`
		Pressure    number `json:"surface_pressure"` // hPa at station level
	} `json:"current"`
}

func (s *OpenMeteo) Fetch(ctx context.Context) ([]domain.Reading, error) {
	lats := make([]string, len(s.Points))
	lons := make([]string, len(s.Points))
	for i, p := range s.Points {
		lats[i] = strconv.FormatFloat(p.Lat, 'f', 4, 64)
		lons[i] = strconv.FormatFloat(p.Lon, 'f', 4, 64)
	}
	query := url.Values{
		"latitude":        {strings.Join(lats, ",")},
		"longitude":       {strings.Join(lons, ",")},
		"current":         {"temperature_2m,relative_humidity_2m,wind_speed_10m,wind_direction_10m,surface_pressure"},
		"wind_speed_unit": {"ms"},
		"timeformat":      {"unixtime"},
		"timezone":        {"GMT"},
	}

	var raw json.RawMessage
	if err := getJSON(ctx, s.Client, s.BaseURL+"/v1/forecast?"+query.Encode(), &raw); err != nil {
		return nil, err
	}
	locations, err := decodeOpenMeteo(raw)
	if err != nil {
		return nil, err
	}
	// The answer has no identifier per location: it follows the order of the
	// request, so a different length means the pairing cannot be trusted.
	if len(locations) != len(s.Points) {
		return nil, fmt.Errorf("asked for %d points, received %d", len(s.Points), len(locations))
	}

	readings := make([]domain.Reading, 0, len(locations))
	for i, loc := range locations {
		if loc.Current == nil || loc.Current.Time == 0 {
			continue
		}
		point := s.Points[i]
		readings = append(readings, domain.Reading{
			InstituteCode: s.Code(),
			ExternalCode:  point.Code,
			StationName:   point.Name + " (modelo)",
			Lat:           point.Lat,
			Lon:           point.Lon,
			AltitudeM:     loc.Elevation.float32Ptr(),
			Observation: domain.Observation{
				ObservedAt:   time.Unix(loc.Current.Time, 0).UTC(),
				TemperatureC: loc.Current.Temperature.float32Ptr(),
				HumidityPct:  loc.Current.Humidity.float32Ptr(),
				WindSpeedMS:  loc.Current.WindSpeed.float32Ptr(),
				WindDirDeg:   loc.Current.WindDir.int16Ptr(),
				PressureHPa:  loc.Current.Pressure.float32Ptr(),
			},
		})
	}
	return readings, nil
}

// decodeOpenMeteo accepts both shapes of the answer: a list when several
// coordinates were requested, a single object when there was only one.
func decodeOpenMeteo(raw json.RawMessage) ([]openMeteoLocation, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []openMeteoLocation
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return list, nil
	}
	var one openMeteoLocation
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return []openMeteoLocation{one}, nil
}
