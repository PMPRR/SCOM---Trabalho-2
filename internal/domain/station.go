package domain

import "time"

type Observation struct {
	ObservedAt		time.Time 	`json:"observed_at"`
	TemperatureC	*float32	`json:"temperature_c"`
	HumidityPct		*float32	`json:"humidity_pct"`
	PrecipitationMM	*float32	`json:"precipitation_mm"`
	WindSpeedMS		*float32	`json:"wind_speed_ms"`
	WindDirDeg		*int16		`json:"wind_dir_deg"`
	PressureHPa		*float32	`json:"pressure_hpa"`
}

type Station struct {
	ID				int64			`json:"id"`
	InstituteCode	string			`json:"institute_code"`
	InstituteName	string			`json:"institute_name"`
	ExternalCode	string			`json:"external_code"`
	Name			string			`json:"name"`
	Lat				float64			`json:"lat"`
	Lon				float64			`json:"lon"`
	AltitudeM		*float32		`json:"altitude_m"`
	Active			bool			`json:"active"`
	OwnerID			*int64			`json:"owner_id"`
	CreatedAt		time.Time		`json:"created_at"`
	UpdatedAt		time.Time		`json:"updated_at"`
	Latest			*Observation	`json:"latest"`
}

