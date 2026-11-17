package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const cemadenRegistry = `[
	{"codestacao":" 355030801A ","id_estacao":4521,"nome":"Jardim Paulistano","cidade":"SÃO PAULO",
	 "uf":"SP","latitude":-23.5712,"longitude":-46.6841,"tipoestacao_descricao":"Pluviométrica"},
	{"codestacao":"350950201A","id_estacao":4600,"nome":"Centro","cidade":"CAMPINAS",
	 "uf":"SP","latitude":"-22.9056","longitude":"-47.0608"}]`

// fakeCemaden imitates the PED: a login route and two data routes that only
// answer to the token it issued.
type fakeCemaden struct {
	t      *testing.T
	valid  string // the token the data routes accept
	issue  string // the token the login hands out
	logins int
	recent string
	srv    *httptest.Server
}

func newFakeCemaden(t *testing.T, recent string) *fakeCemaden {
	f := &fakeCemaden{t: t, valid: "jwt-novo", issue: "jwt-novo", recent: recent}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "ana@example.com" || body["password"] != "segredo" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.logins++
		_ = json.NewEncoder(w).Encode(map[string]string{"token": f.issue})
	})
	data := func(body func() string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("token") != f.valid {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`[{"Alerta":"Token expirado"}]`))
				return
			}
			if r.URL.Query().Get("uf") != "SP" {
				t.Errorf("uf = %q, want SP", r.URL.Query().Get("uf"))
			}
			_, _ = w.Write([]byte(body()))
		}
	}
	mux.HandleFunc("GET /data/pcds/pcds-dados-recentes", data(func() string { return f.recent }))
	mux.HandleFunc("GET /data/pcds-cadastro/dados-cadastrais", data(func() string { return cemadenRegistry }))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCemaden) source(email, password, token string) *CEMADEN {
	s := NewCEMADEN(f.srv.Client(), email, password, token)
	s.DataURL, s.AuthURL = f.srv.URL+"/data", f.srv.URL+"/auth"
	return s
}

const cemadenRecent = `[
	{"codestacao":"355030801A","datahora":"2026-11-16 20:50:00.0","id_sensor":10,"valor":4.0},
	{"codestacao":"355030801A","datahora":"2026-11-16 21:10:00.0","id_sensor":10,"valor":1.2},
	{"codestacao":"355030801A","datahora":"2026-11-16 21:40:00.0","id_sensor":10,"valor":0.6},
	{"codestacao":"355030801A","datahora":"2026-11-16 21:50:00.0","id_sensor":10,"valor":-1},
	{"codestacao":"355030801A","datahora":"2026-11-16 22:00:00.0","id_sensor":10,"valor":0.2},
	{"codestacao":"350950201A","datahora":"2026-11-16 22:00","valor":"0"},
	{"codestacao":"350950201A","datahora":"2026-11-16 22:00","id_sensor":20,"valor":3.5},
	{"codestacao":"999999999Z","datahora":"2026-11-16 22:00","id_sensor":10,"valor":7.0}]`

func TestCEMADENLogsInAndSumsTheLastHour(t *testing.T) {
	fake := newFakeCemaden(t, cemadenRecent)

	readings, err := fake.source("ana@example.com", "segredo", "").Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.logins != 1 {
		t.Errorf("%d logins, want 1", fake.logins)
	}
	got := byCode(readings)
	if len(got) != 2 {
		t.Fatalf("%d stations, want 2 (the one missing from the registry has no position)", len(got))
	}

	sp := got["355030801A"]
	if sp.InstituteCode != "cemaden" || sp.StationName != "Jardim Paulistano (SÃO PAULO)" {
		t.Errorf("station = %+v", sp)
	}
	if sp.Lat != -23.5712 || sp.Lon != -46.6841 {
		t.Errorf("position = %v, %v", sp.Lat, sp.Lon)
	}
	// 21:10 + 21:40 + 22:00; 20:50 is more than an hour old and -1 is missing.
	if !near(sp.Observation.PrecipitationMM, 2.0) {
		t.Errorf("rain = %v mm, want 2.0", *sp.Observation.PrecipitationMM)
	}
	if want := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC); !sp.Observation.ObservedAt.Equal(want) {
		t.Errorf("observed at %v, want %v", sp.Observation.ObservedAt, want)
	}
	if sp.Observation.TemperatureC != nil || sp.Observation.WindSpeedMS != nil {
		t.Errorf("a rain gauge reports rain only: %+v", sp.Observation)
	}

	// Zero is a real reading (it did not rain); the level sensor is ignored.
	if camp := got["350950201A"]; !near(camp.Observation.PrecipitationMM, 0) || camp.Lat != -22.9056 {
		t.Errorf("Campinas = %+v", camp)
	}
}

func TestCEMADENRenewsAnExpiredTokenOnce(t *testing.T) {
	fake := newFakeCemaden(t, cemadenRecent)

	// The source starts with a token that has already expired.
	readings, err := fake.source("ana@example.com", "segredo", "jwt-velho").Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.logins != 1 || len(readings) != 2 {
		t.Errorf("%d logins and %d readings, want 1 and 2", fake.logins, len(readings))
	}
}

func TestCEMADENStaticTokenWorksWithoutLogin(t *testing.T) {
	fake := newFakeCemaden(t, cemadenRecent)

	readings, err := fake.source("", "", "jwt-novo").Fetch(context.Background())
	if err != nil || len(readings) != 2 {
		t.Fatalf("readings = %d, err = %v", len(readings), err)
	}
	if fake.logins != 0 {
		t.Errorf("%d logins, want none", fake.logins)
	}
}

func TestCEMADENExpiredStaticTokenSaysWhatToDo(t *testing.T) {
	fake := newFakeCemaden(t, cemadenRecent)

	_, err := fake.source("", "", "jwt-velho").Fetch(context.Background())
	if err == nil {
		t.Fatal("want an error for an expired token that cannot be renewed")
	}
	if !strings.Contains(err.Error(), "CEMADEN_EMAIL") {
		t.Errorf("error should point to the fix: %v", err)
	}
	if strings.Contains(err.Error(), "jwt-velho") {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestCEMADENWrongPasswordDoesNotLeakIt(t *testing.T) {
	fake := newFakeCemaden(t, cemadenRecent)

	_, err := fake.source("ana@example.com", "senha-errada", "").Fetch(context.Background())
	if err == nil {
		t.Fatal("want an error when the login is refused")
	}
	if strings.Contains(err.Error(), "senha-errada") || strings.Contains(err.Error(), "ana@example.com") {
		t.Errorf("error leaks the credentials: %v", err)
	}
}

func TestCEMADENAcceptsWrappedAnswerAndOtherKeySpellings(t *testing.T) {
	fake := newFakeCemaden(t, `{"data":[
		{"codEstacao":"355030801A","dataHora":"2026-11-16T22:00:00","sensor":10,"valor":"1.5"}]}`)

	readings, err := fake.source("", "", "jwt-novo").Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(readings) != 1 || !near(readings[0].Observation.PrecipitationMM, 1.5) {
		t.Errorf("readings = %+v", readings)
	}
}

func TestCEMADENNoRecentReadings(t *testing.T) {
	fake := newFakeCemaden(t, `[]`)

	readings, err := fake.source("", "", "jwt-novo").Fetch(context.Background())
	if err != nil || len(readings) != 0 {
		t.Errorf("readings = %v, err = %v; want none and no error", readings, err)
	}
}
