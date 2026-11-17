package domain

// Reading is one observation as delivered by an external source, already
// converted to the units of the database (°C, %, mm, m/s, degrees, hPa). It
// carries the station it belongs to, because a source may report a station
// the system has never seen.
type Reading struct {
	InstituteCode string // institutes.code, e.g. "cemaden"
	ExternalCode  string // the station's code at the source, e.g. "355030801A"
	StationName   string
	Lat           float64
	Lon           float64
	AltitudeM     *float32
	Observation   Observation
}
