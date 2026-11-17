package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

const (
	cemadenRainSensor = 10 // sensor code of the rain gauge
	cemadenNetwork    = 11 // network of the automatic rain gauges
)

// CEMADEN reads the rain gauges of the State from the PED (Plataforma de
// Entrega de Dados) of CEMADEN: the densest rain network available, with a
// reading every 10 minutes. It reports rain only.
//
// The use of this data requires the credit "Dados da Rede Observacional do
// CEMADEN/MCTIC" wherever it is shown, and allows 12 requests per minute.
// One collection makes at most four.
//
// The API layout follows the hydrobr library (github.com/wallissoncarvalho/
// hydrobr), which is the reference used here. Field names are matched
// loosely, because the PED is not consistent about them.
type CEMADEN struct {
	Client   *http.Client
	DataURL  string
	AuthURL  string
	Email    string
	Password string
	State    string

	mu    sync.Mutex
	token string
}

// NewCEMADEN builds the source. With e-mail and password the token is
// obtained and renewed automatically; with a token alone it is used as it
// is, until it expires.
func NewCEMADEN(client *http.Client, email, password, token string) *CEMADEN {
	return &CEMADEN{
		Client:   client,
		DataURL:  "https://sws.cemaden.gov.br/PED/rest",
		AuthURL:  "https://ped.cemaden.gov.br/SGAA/rest/controle-token/tokens",
		Email:    email,
		Password: password,
		State:    "SP",
		token:    token,
	}
}

func (s *CEMADEN) Code() string { return "cemaden" }

// canLogin reports whether the token can be renewed without a person.
func (s *CEMADEN) canLogin() bool { return s.Email != "" && s.Password != "" }

func (s *CEMADEN) Fetch(ctx context.Context) ([]domain.Reading, error) {
	recent, err := s.get(ctx, "/pcds/pcds-dados-recentes", url.Values{
		"uf":      {s.State},
		"rede":    {fmt.Sprint(cemadenNetwork)},
		"sensor":  {fmt.Sprint(cemadenRainSensor)},
		"formato": {"JSON"},
	})
	if err != nil {
		return nil, fmt.Errorf("recent readings: %w", err)
	}
	if len(recent) == 0 {
		return nil, nil
	}

	registry, err := s.get(ctx, "/pcds-cadastro/dados-cadastrais", url.Values{
		"uf":      {s.State},
		"formato": {"JSON"},
	})
	if err != nil {
		return nil, fmt.Errorf("station registry: %w", err)
	}

	return cemadenReadings(s.Code(), recent, registry), nil
}

// get asks the data service, logging in first when there is no token and
// once more when the service says the token is no longer valid.
func (s *CEMADEN) get(ctx context.Context, path string, query url.Values) ([]cemadenRecord, error) {
	token, err := s.currentToken(ctx)
	if err != nil {
		return nil, err
	}

	var raw json.RawMessage
	endpoint := s.DataURL + path + "?" + query.Encode()
	err = requestJSON(ctx, s.Client, http.MethodGet, endpoint, map[string]string{"token": token}, nil, &raw)

	var status *statusError
	if errors.As(err, &status) && status.Code == http.StatusUnauthorized {
		if !s.canLogin() {
			return nil, errors.New("token refused or expired (HTTP 401): generate a new one, " +
				"or set CEMADEN_EMAIL and CEMADEN_PASSWORD so it is renewed automatically")
		}
		if token, err = s.login(ctx); err != nil {
			return nil, err
		}
		raw = nil
		err = requestJSON(ctx, s.Client, http.MethodGet, endpoint, map[string]string{"token": token}, nil, &raw)
	}
	if err != nil {
		return nil, err
	}
	return decodeCemaden(raw)
}

func (s *CEMADEN) currentToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if token != "" {
		return token, nil
	}
	return s.login(ctx)
}

// login exchanges e-mail and password for a token and keeps it for the
// next collections.
func (s *CEMADEN) login(ctx context.Context) (string, error) {
	if !s.canLogin() {
		return "", errors.New("no credentials: set CEMADEN_EMAIL and CEMADEN_PASSWORD, or CEMADEN_TOKEN")
	}

	var answer struct {
		Token string `json:"token"`
	}
	credentials := map[string]string{"email": s.Email, "password": s.Password}
	if err := requestJSON(ctx, s.Client, http.MethodPost, s.AuthURL, nil, credentials, &answer); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if answer.Token == "" {
		return "", errors.New("login: the answer carries no token")
	}

	s.mu.Lock()
	s.token = answer.Token
	s.mu.Unlock()
	return answer.Token, nil
}

// cemadenRecord is one object of an answer, with its keys reduced to lower
// case letters and digits: "codEstacao", "cod_estacao" and "codestacao" are
// the same key.
type cemadenRecord map[string]json.RawMessage

func (r cemadenRecord) text(keys ...string) string {
	for _, key := range keys {
		raw, ok := r[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if text = strings.TrimSpace(text); text != "" {
				return text
			}
			continue
		}
		// A code may come as a bare number.
		if bare := strings.TrimSpace(string(raw)); bare != "null" && bare != "" {
			return bare
		}
	}
	return ""
}

func (r cemadenRecord) number(keys ...string) number {
	for _, key := range keys {
		var n number
		if raw, ok := r[key]; ok && n.UnmarshalJSON(raw) == nil && n.ok {
			return n
		}
	}
	return number{}
}

// decodeCemaden accepts the shapes the PED uses for a list: a JSON array,
// an object with the array under "data", or a single object.
func decodeCemaden(raw json.RawMessage) ([]cemadenRecord, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var objects []map[string]json.RawMessage
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(raw, &objects); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
	} else {
		var single map[string]json.RawMessage
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if data, wrapped := single["data"]; wrapped {
			return decodeCemaden(data)
		}
		objects = []map[string]json.RawMessage{single}
	}

	records := make([]cemadenRecord, 0, len(objects))
	for _, object := range objects {
		record := make(cemadenRecord, len(object))
		for key, value := range object {
			record[normalizeKey(key)] = value
		}
		records = append(records, record)
	}
	return records, nil
}

func normalizeKey(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cemadenTimeLayouts are the date formats seen in PED answers, all in UTC.
// Go accepts fractional seconds after the seconds field even when the layout
// does not show them.
var cemadenTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04:05",
	time.RFC3339,
}

func parseCemadenTime(text string) (time.Time, bool) {
	for _, layout := range cemadenTimeLayouts {
		if at, err := time.Parse(layout, text); err == nil {
			return at.UTC(), true
		}
	}
	return time.Time{}, false
}

type cemadenStation struct {
	name string
	lat  number
	lon  number
	alt  number
}

type cemadenGauge struct {
	at    time.Time
	value float64
}

// cemadenReadings turns the 10-minute readings into one reading per station:
// the rain of the hour that ends at its latest reading. That is the quantity
// the other sources report and the one the site shows.
func cemadenReadings(institute string, recent, registry []cemadenRecord) []domain.Reading {
	stations := make(map[string]cemadenStation, len(registry))
	for _, rec := range registry {
		code := rec.text("codestacao")
		if code == "" {
			continue
		}
		stations[code] = cemadenStation{
			name: cemadenName(rec, code),
			lat:  rec.number("latitude"),
			lon:  rec.number("longitude"),
			alt:  rec.number("altitude"),
		}
	}

	gauges := make(map[string][]cemadenGauge)
	for _, rec := range recent {
		code := rec.text("codestacao")
		value := rec.number("valor")
		at, ok := parseCemadenTime(rec.text("datahora"))
		// A negative value (-1) is how the PED marks a missing reading.
		if code == "" || !ok || !value.ok || value.value < 0 {
			continue
		}
		if sensor := rec.number("idsensor", "sensor"); sensor.ok && int(sensor.value) != cemadenRainSensor {
			continue
		}
		gauges[code] = append(gauges[code], cemadenGauge{at: at, value: value.value})

		// A station missing from the registry can still be placed when the
		// reading itself carries the position.
		if _, known := stations[code]; !known {
			stations[code] = cemadenStation{
				name: cemadenName(rec, code),
				lat:  rec.number("latitude"),
				lon:  rec.number("longitude"),
			}
		}
	}

	readings := make([]domain.Reading, 0, len(gauges))
	for code, series := range gauges {
		station := stations[code]
		if !station.lat.ok || !station.lon.ok {
			continue
		}

		latest := series[0].at
		for _, g := range series {
			if g.at.After(latest) {
				latest = g.at
			}
		}
		var hour float64
		for _, g := range series {
			if g.at.After(latest.Add(-time.Hour)) {
				hour += g.value
			}
		}
		rain := float32(hour)

		readings = append(readings, domain.Reading{
			InstituteCode: institute,
			ExternalCode:  code,
			StationName:   station.name,
			Lat:           station.lat.value,
			Lon:           station.lon.value,
			AltitudeM:     station.alt.float32Ptr(),
			Observation:   domain.Observation{ObservedAt: latest, PrecipitationMM: &rain},
		})
	}
	return readings
}

// cemadenName builds "Nome (Cidade)" from what the record has.
func cemadenName(rec cemadenRecord, code string) string {
	name, city := rec.text("nome"), rec.text("cidade")
	switch {
	case name != "" && city != "":
		return name + " (" + city + ")"
	case name != "":
		return name
	case city != "":
		return city
	default:
		return code
	}
}
