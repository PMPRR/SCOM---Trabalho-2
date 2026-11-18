// Command fakestation is a simulated private weather station for demos and
// tests. It behaves like a real one built with the guide in
// docs/estacao-particular.md:
//
//  1. creates (once) an Ed25519 key pair and keeps it in DATA_DIR;
//  2. logs in to the site with its own account, signing up the first time;
//  3. registers its station (or finds it again) with the public key and the
//     report interval;
//  4. every interval, signs a JSON reading with the private key and sends it
//     to POST /api/stations/{id}/readings.
//
// The readings are made up: a daily temperature cycle with noise, humidity
// that follows it, pressure for the station altitude, wind and the odd
// shower. The default position is the UNESP campus in Sorocaba (ICTS),
// taken from the IBGE geodetic mark SPS1 on the laboratories building.
//
// It uses only the standard library. Configuration, all optional:
//
//	SERVER_URL        site address              (http://app:8080)
//	STATION_CODE      station code              (unesp-sorocaba)
//	STATION_NAME      station name              (UNESP Sorocaba (estação simulada))
//	STATION_LAT       latitude                  (-23.479191)
//	STATION_LON       longitude                 (-47.416835)
//	STATION_ALT       altitude in metres        (625)
//	STATION_INTERVAL  seconds between readings  (60, the minimum the site accepts)
//	STATION_EMAIL     account e-mail            (estacao.unesp@example.com)
//	STATION_PASSWORD  account password          (made up and kept in DATA_DIR)
//	DATA_DIR          where key and state live  (/data)
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // time zones inside the binary
)

type config struct {
	ServerURL string
	Code      string
	Name      string
	Lat, Lon  float64
	Alt       float64
	Interval  time.Duration
	Email     string
	Password  string
	DataDir   string
}

func loadConfig() (config, error) {
	c := config{
		ServerURL: strings.TrimRight(env("SERVER_URL", "http://app:8080"), "/"),
		Code:      env("STATION_CODE", "unesp-sorocaba"),
		Name:      env("STATION_NAME", "UNESP Sorocaba (estação simulada)"),
		Email:     env("STATION_EMAIL", "estacao.unesp@example.com"),
		Password:  os.Getenv("STATION_PASSWORD"),
		DataDir:   env("DATA_DIR", "/data"),
	}
	var err error
	if c.Lat, err = envFloat("STATION_LAT", -23.479191); err != nil {
		return c, err
	}
	if c.Lon, err = envFloat("STATION_LON", -47.416835); err != nil {
		return c, err
	}
	if c.Alt, err = envFloat("STATION_ALT", 625); err != nil {
		return c, err
	}
	secs, err := envFloat("STATION_INTERVAL", 60)
	if err != nil {
		return c, err
	}
	if secs < 60 || secs > 86400 {
		return c, errors.New("STATION_INTERVAL must be between 60 and 86400 seconds")
	}
	c.Interval = time.Duration(secs) * time.Second
	return c, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := loadConfig()
	if err != nil {
		log.Error("bad configuration", "error", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := &station{cfg: cfg, log: log, http: &http.Client{
		Timeout: 20 * time.Second,
		// Login answers with a redirect; the cookies are in that answer.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, weather: newWeather(cfg.Alt)}
	if err := st.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("station stopped", "error", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- state

// state is what survives a restart, kept in DATA_DIR/state.json. The private
// key lives apart, in DATA_DIR/key, readable only by this user.
type state struct {
	Password  string `json:"password,omitempty"`
	StationID int64  `json:"station_id,omitempty"`
}

type station struct {
	cfg     config
	log     *slog.Logger
	http    *http.Client
	weather *weather

	key     ed25519.PrivateKey
	state   state
	cookies map[string]string
}

func (s *station) run(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := s.loadKey(); err != nil {
		return err
	}
	s.loadState()
	s.log.Info("station starting", "code", s.cfg.Code, "lat", s.cfg.Lat, "lon", s.cfg.Lon,
		"interval", s.cfg.Interval, "server", s.cfg.ServerURL, "key", publicKeyLine(s.key)[:40]+"...")

	for {
		if err := s.setup(ctx); err != nil {
			s.log.Warn("setup failed, trying again in 15s", "error", err)
			if !sleep(ctx, 15*time.Second) {
				return ctx.Err()
			}
			continue
		}
		// send returns when the station must be registered again.
		if err := s.send(ctx); err != nil {
			return err
		}
	}
}

func (s *station) loadKey() error {
	path := filepath.Join(s.cfg.DataDir, "key")
	if raw, err := os.ReadFile(path); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return fmt.Errorf("%s is not a key made by this program", path)
		}
		s.key = ed25519.NewKeyFromSeed(seed)
		return nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	s.key = priv
	s.log.Info("new key pair created", "file", path)
	return os.WriteFile(path, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600)
}

func (s *station) loadState() {
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "state.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &s.state)
	}
}

func (s *station) saveState() {
	raw, _ := json.MarshalIndent(s.state, "", "  ")
	if err := os.WriteFile(filepath.Join(s.cfg.DataDir, "state.json"), raw, 0o600); err != nil {
		s.log.Warn("could not save state", "error", err)
	}
}

func (s *station) password() string {
	if s.cfg.Password != "" {
		return s.cfg.Password
	}
	if s.state.Password == "" {
		buf := make([]byte, 18)
		_, _ = rand.Read(buf)
		s.state.Password = base64.RawURLEncoding.EncodeToString(buf)
		s.saveState()
	}
	return s.state.Password
}

// ---------------------------------------------------------------- account and station

// setup logs in (or signs up) and makes sure the station exists on the site
// with this program's public key.
func (s *station) setup(ctx context.Context) error {
	s.cookies = map[string]string{}
	if err := s.login(ctx); err != nil {
		return err
	}
	id, err := s.findStation(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{
		"external_code":     s.cfg.Code,
		"name":              s.cfg.Name,
		"lat":               s.cfg.Lat,
		"lon":               s.cfg.Lon,
		"altitude_m":        s.cfg.Alt,
		"active":            true,
		"public_key":        publicKeyLine(s.key),
		"report_interval_s": int(s.cfg.Interval / time.Second),
	}
	if id == 0 {
		var out struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		if err := s.api(ctx, http.MethodPost, "/api/stations", body, &out); err != nil {
			return fmt.Errorf("register station: %w", err)
		}
		id = out.Data.ID
		s.log.Info("station registered", "station_id", id)
	} else {
		// Same station found again: make sure it has this key and interval.
		if err := s.api(ctx, http.MethodPut, fmt.Sprintf("/api/stations/%d", id), body, nil); err != nil {
			return fmt.Errorf("update station %d: %w", id, err)
		}
		s.log.Info("station found again", "station_id", id)
	}
	s.state.StationID = id
	s.saveState()
	return nil
}

func (s *station) login(ctx context.Context) error {
	// Any GET hands out the CSRF cookie that the forms must repeat.
	if _, err := s.do(ctx, http.MethodGet, "/entrar", nil, ""); err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	form := url.Values{"email": {s.cfg.Email}, "senha": {s.password()}, "csrf_token": {s.cookies["climasp_csrf"]}}
	res, err := s.do(ctx, http.MethodPost, "/entrar", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusSeeOther && s.cookies["climasp_session"] != "" {
		s.log.Info("logged in", "email", s.cfg.Email)
		return nil
	}

	// No account yet (or a fresh database): sign up, which also logs in.
	form = url.Values{
		"nome": {s.cfg.Name}, "email": {s.cfg.Email},
		"senha": {s.password()}, "senha-confirma": {s.password()},
		"termos": {"on"}, "csrf_token": {s.cookies["climasp_csrf"]},
	}
	res, err = s.do(ctx, http.MethodPost, "/cadastro", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusSeeOther && s.cookies["climasp_session"] != "" {
		s.log.Info("account created", "email", s.cfg.Email)
		return nil
	}
	if res.StatusCode == http.StatusConflict {
		return fmt.Errorf("the account %s exists with another password: set STATION_PASSWORD or delete it", s.cfg.Email)
	}
	return fmt.Errorf("sign up answered %d", res.StatusCode)
}

// findStation returns the id of this station on the site, or 0 if there is
// none. The id saved in state.json comes first; after a database reset it
// is gone, and then the public list is searched by code.
func (s *station) findStation(ctx context.Context) (int64, error) {
	type item struct {
		ID            int64  `json:"id"`
		InstituteCode string `json:"institute_code"`
		ExternalCode  string `json:"external_code"`
	}
	if s.state.StationID != 0 {
		var out struct {
			Data item `json:"data"`
		}
		err := s.api(ctx, http.MethodGet, fmt.Sprintf("/api/stations/%d", s.state.StationID), nil, &out)
		if err == nil && out.Data.ExternalCode == s.cfg.Code {
			return out.Data.ID, nil
		}
	}
	for offset := 0; ; offset += 500 {
		var out struct {
			Data []item `json:"data"`
		}
		if err := s.api(ctx, http.MethodGet, fmt.Sprintf("/api/stations?limit=500&offset=%d", offset), nil, &out); err != nil {
			return 0, fmt.Errorf("list stations: %w", err)
		}
		for _, it := range out.Data {
			if it.InstituteCode == "particular" && it.ExternalCode == s.cfg.Code {
				return it.ID, nil
			}
		}
		if len(out.Data) < 500 {
			return 0, nil
		}
	}
}

// ---------------------------------------------------------------- readings

// send posts one signed reading per interval. It returns nil when the site
// no longer knows the station (deleted, key changed, database reset), so
// that run registers it again.
func (s *station) send(ctx context.Context) error {
	path := fmt.Sprintf("/api/stations/%d/readings", s.state.StationID)
	next := time.Now()
	failures := 0
	for {
		if !sleep(ctx, time.Until(next)) {
			return ctx.Err()
		}
		now := time.Now().UTC().Truncate(time.Second)
		body, err := json.Marshal(s.weather.reading(now, s.cfg.Interval))
		if err != nil {
			return err
		}
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(s.key, body))

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ServerURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature", sig)
		res, err := s.http.Do(req)
		if err != nil {
			failures++
			s.log.Warn("send failed", "error", err)
			next = time.Now().Add(backoff(failures))
			continue
		}
		answer, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()

		switch {
		case res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusOK:
			failures = 0
			s.log.Info("reading sent", "station_id", s.state.StationID, "status", res.StatusCode, "reading", string(body))
			next = now.Add(s.cfg.Interval)
		case res.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(res.Header.Get("Retry-After"), s.cfg.Interval)
			s.log.Info("too soon, waiting", "wait", wait)
			next = time.Now().Add(wait)
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden ||
			res.StatusCode == http.StatusNotFound:
			// Key no longer matches, uplink disabled or station gone.
			s.log.Warn("the site refused the station, registering again", "status", res.StatusCode, "answer", string(answer))
			return nil
		default:
			failures++
			s.log.Warn("reading refused", "status", res.StatusCode, "answer", string(answer))
			next = time.Now().Add(backoff(failures))
		}
	}
}

// weather makes up plausible readings for Sorocaba in late spring.
type weather struct {
	rnd      *mrand.Rand
	basePres float64 // sea-level pressure brought to the station altitude
	drift    float64 // slow day-to-day change of the mean temperature
	windDir  float64
	raining  bool
}

func newWeather(altitude float64) *weather {
	return &weather{
		rnd:      mrand.New(mrand.NewPCG(uint64(time.Now().UnixNano()), 7)),
		basePres: 1013.25 * math.Exp(-altitude/8434),
		windDir:  120, // south-easterly, the usual wind in the region
	}
}

type reading struct {
	ObservedAt      time.Time `json:"observed_at"`
	TemperatureC    float64   `json:"temperature_c"`
	HumidityPct     float64   `json:"humidity_pct"`
	PrecipitationMM float64   `json:"precipitation_mm"`
	WindSpeedMS     float64   `json:"wind_speed_ms"`
	WindDirDeg      float64   `json:"wind_dir_deg"`
	PressureHPa     float64   `json:"pressure_hpa"`
}

func (w *weather) reading(at time.Time, interval time.Duration) reading {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*3600)
	}
	local := at.In(loc)
	hour := float64(local.Hour()) + float64(local.Minute())/60
	w.drift = clamp(w.drift+w.rnd.NormFloat64()*0.05, -3, 3)

	// Coolest around 6 h, warmest around 15 h.
	temp := 22 + w.drift + 5.5*math.Cos(2*math.Pi*(hour-15)/24) + w.rnd.NormFloat64()*0.3

	// Showers start more often in the afternoon and stop on their own.
	start := 0.002
	if hour >= 14 && hour <= 20 {
		start = 0.01
	}
	if w.raining {
		w.raining = w.rnd.Float64() < 0.9
	} else {
		w.raining = w.rnd.Float64() < start
	}
	rain := 0.0
	if w.raining {
		rain = math.Round(w.rnd.Float64()*0.4*interval.Minutes()*10) / 10
		temp -= 2
	}

	hum := 70 - 2.8*(temp-22) + w.rnd.NormFloat64()*2
	if w.raining {
		hum += 15
	}
	daytime := math.Max(0, math.Sin(math.Pi*(hour-7)/12))
	wind := math.Max(0, 1.2+2.3*daytime+w.rnd.NormFloat64()*0.6)
	w.windDir = math.Mod(w.windDir+w.rnd.NormFloat64()*8+360, 360)
	// The air pressure rises and falls twice a day (atmospheric tide).
	pres := w.basePres + 1.2*math.Cos(2*math.Pi*(hour-10)/12) + w.rnd.NormFloat64()*0.2

	return reading{
		ObservedAt:      at,
		TemperatureC:    round1(temp),
		HumidityPct:     round1(clamp(hum, 25, 100)),
		PrecipitationMM: rain,
		WindSpeedMS:     round1(wind),
		WindDirDeg:      math.Round(w.windDir),
		PressureHPa:     round1(pres),
	}
}

// ---------------------------------------------------------------- HTTP helpers

// do sends a request with the cookies collected so far and keeps the new
// ones. Cookies are handled by hand because the site marks them Secure
// outside development, and a cookie jar would not send them over plain HTTP
// inside the container network.
func (s *station) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.ServerURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range s.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	if token := s.cookies["climasp_csrf"]; token != "" && method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", token)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	for _, c := range res.Cookies() {
		s.cookies[c.Name] = c.Value
	}
	return res, nil
}

// api sends JSON (or nothing) and decodes a JSON answer into out.
func (s *station) api(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	contentType := ""
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
		contentType = "application/json"
	}
	res, err := s.do(ctx, method, path, body, contentType)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s answered %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// publicKeyLine writes the public key in the OpenSSH format the site takes,
// the same line ssh-keygen puts in a .pub file.
func publicKeyLine(priv ed25519.PrivateKey) string {
	pub := priv.Public().(ed25519.PublicKey)
	var blob []byte
	blob = binary.BigEndian.AppendUint32(blob, uint32(len("ssh-ed25519")))
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(len(pub)))
	blob = append(blob, pub...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " climasp-fakestation"
}

// ---------------------------------------------------------------- small helpers

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) (float64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(strings.Replace(v, ",", ".", 1), 64)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a number", key, v)
	}
	return f, nil
}

func retryAfter(header string, fallback time.Duration) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs >= 0 {
		return time.Duration(secs)*time.Second + 500*time.Millisecond
	}
	return fallback
}

func backoff(failures int) time.Duration {
	d := time.Duration(1<<min(failures, 6)) * time.Second
	return min(d, time.Minute)
}

// sleep waits d or until ctx ends; it reports whether to go on.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func round1(v float64) float64        { return math.Round(v*10) / 10 }
