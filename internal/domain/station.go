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

	// Uplink: a private station may send its own readings, signed with an
	// Ed25519 key. PublicKey is the OpenSSH line of the public half (it is
	// public by nature); empty means the station does not send.
	PublicKey       string     `json:"public_key,omitempty"`
	KeyFingerprint  string     `json:"key_fingerprint,omitempty"`
	ReportIntervalS *int32     `json:"report_interval_s"`
	LastReportAt    *time.Time `json:"last_report_at"`
	// UplinkStatus is derived by the service: "waiting" before the first
	// submission, "ok" while they arrive on time, "late" after two missed
	// intervals; empty for a station without a key.
	UplinkStatus UplinkStatus `json:"uplink_status,omitempty"`
}

// UplinkStatus says whether a station that sends its own readings is doing so.
type UplinkStatus string

const (
	UplinkWaiting UplinkStatus = "waiting"
	UplinkOK      UplinkStatus = "ok"
	UplinkLate    UplinkStatus = "late"
)

// OperatorInstitute is the institutes.code of the stations registered by
// hand on the site. Their owner is the user who registered them.
const OperatorInstitute = "particular"

// IsOfficial reports whether the station comes from an institute's network
// (it has no owner). Its data is kept up to date by the collection, so on
// the site it can only be turned on or off, never edited or deleted.
func (s Station) IsOfficial() bool {
	return s.OwnerID == nil
}

// StationInput is what a user sends to register a station or change one.
// PublicKey and ReportIntervalS go together: both set, the station may send
// its readings; both empty, it does not. KeyFingerprint is filled in by the
// service from the key.
type StationInput struct {
	ExternalCode    string
	Name            string
	Lat             float64
	Lon             float64
	AltitudeM       *float32
	Active          bool
	PublicKey       string
	ReportIntervalS int32
	KeyFingerprint  string
}

// SubmissionResult is what happened to a reading sent by a station.
type SubmissionResult struct {
	Stored     bool      // false: a reading with the same time was already stored
	ObservedAt time.Time // the time the reading was stored under
	NextAfter  time.Time // the earliest time the next submission is accepted
}

// StationFilter narrows a listing of stations. The zero value lists all.
type StationFilter struct {
	ActiveOnly  bool   // leave out stations turned off
	OwnerID     *int64 // only the stations of this user
	Search      string // part of the name or of the code, any case
	NewestFirst bool   // most recent registration first, instead of by id
}
