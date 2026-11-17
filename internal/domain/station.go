// Package domain holds the entities of the system. It knows nothing about
// SQL or HTTP, so every other layer may depend on it.
package domain

import "time"

// Observation is one reading of a station. Sensors a station does not have
// stay nil and are serialized as null.
type Observation struct {
	ObservedAt      time.Time `json:"observed_at"`
	TemperatureC    *float32  `json:"temperature_c"`
	HumidityPct     *float32  `json:"humidity_pct"`
	PrecipitationMM *float32  `json:"precipitation_mm"`
	WindSpeedMS     *float32  `json:"wind_speed_ms"`
	WindDirDeg      *int16    `json:"wind_dir_deg"`
	PressureHPa     *float32  `json:"pressure_hpa"`
}

// Confidence says how much the latest reading of a station can be trusted.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Station is a weather station with its most recent observation, if any.
// OwnerID is the operator who registered it; nil means an official station
// of an institute, managed only by administrators. Confidence is derived by
// the service layer and is empty while the station has no reading.
type Station struct {
	ID            int64        `json:"id"`
	InstituteCode string       `json:"institute_code"`
	InstituteName string       `json:"institute_name"`
	ExternalCode  string       `json:"external_code"`
	Name          string       `json:"name"`
	Lat           float64      `json:"lat"`
	Lon           float64      `json:"lon"`
	AltitudeM     *float32     `json:"altitude_m"`
	Active        bool         `json:"active"`
	OwnerID       *int64       `json:"owner_id"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Latest        *Observation `json:"latest"`
	Confidence    Confidence   `json:"confidence"`
}

