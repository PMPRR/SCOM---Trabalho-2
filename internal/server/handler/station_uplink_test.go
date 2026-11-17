package handler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These tests play the station: a plain HTTP client, no cookies, no CSRF
// token, only the signature of the body.

var deviceKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))

func deviceKeyLine(key ed25519.PrivateKey) string {
	str := func(b []byte) []byte { return append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...) }
	blob := append(str([]byte("ssh-ed25519")), str(key.Public().(ed25519.PublicKey))...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " estacao"
}

// device sends a reading the way a station does.
func (s *site) device(id, body, signature, contentType string) reply {
	s.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.url+"/api/stations/"+id+"/readings", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if signature != "" {
		req.Header.Set("X-Signature", signature)
	}
	resp, err := http.DefaultClient.Do(req) // no cookie jar: a device has no session
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, body: string(data)}
}

func signed(key ed25519.PrivateKey, body string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(body)))
}

// stationWithKey registers, as Ana, a station that sends every 10 minutes.
func stationWithKey(t *testing.T, s *site) string {
	t.Helper()
	s.signup("Ana", "ana@example.com")
	payload, _ := json.Marshal(map[string]any{
		"external_code": "sitio-01", "name": "Sítio", "lat": -23.5, "lon": -46.6,
		"public_key": deviceKeyLine(deviceKey), "report_interval_s": 600,
	})
	r := s.api(http.MethodPost, "/api/stations", string(payload))
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	if !strings.Contains(r.body, `"key_fingerprint":"SHA256:`) || !strings.Contains(r.body, `"uplink_status":"waiting"`) {
		t.Errorf("create response lacks the key: %s", r.body)
	}
	return itoa(dataID(t, r.body))
}

func TestStationSendsSignedReadings(t *testing.T) {
	s := newSite(t)
	id := stationWithKey(t, s)
	body := `{"temperature_c": 22.4, "humidity_pct": 81}`

	first := s.device(id, body, signed(deviceKey, body), "application/json")
	if first.status != http.StatusCreated || !strings.Contains(first.body, `"stored":true`) || !strings.Contains(first.body, `"next_after"`) {
		t.Fatalf("first: %d %s", first.status, first.body)
	}

	// The map and the public API now see the reading.
	if r := s.get("/api/stations/"+id, false); !strings.Contains(r.body, `"temperature_c":22.4`) {
		t.Errorf("public station lacks the reading: %s", r.body)
	}

	// A new reading (its own time) before the interval: 429 with Retry-After.
	next := `{"observed_at": "` + time.Now().UTC().Add(time.Minute).Format(time.RFC3339) + `", "temperature_c": 22.6}`
	soon := s.device(id, next, signed(deviceKey, next), "application/json")
	if soon.status != http.StatusTooManyRequests || soon.header.Get("Retry-After") == "" {
		t.Errorf("too soon: %d, Retry-After %q", soon.status, soon.header.Get("Retry-After"))
	}
	if code, _ := errorField(t, soon.body); code != "too_soon" {
		t.Errorf("too soon: code %q", code)
	}

	// The panel shows the station receiving.
	if panel := s.get("/painel/estacoes", false); !strings.Contains(panel.body, "envio em dia") {
		t.Error("the panel does not show the station sending")
	}
}

func TestStationSubmissionRefusals(t *testing.T) {
	s := newSite(t)
	id := stationWithKey(t, s)
	body := `{"temperature_c": 22.4}`
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize))

	cases := []struct {
		name, id, body, signature, contentType string
		status                                 int
		code                                   string
	}{
		{"no signature", id, body, "", "application/json", 401, "invalid_signature"},
		{"another key", id, body, signed(other, body), "application/json", 401, "invalid_signature"},
		{"body changed", id, `{"temperature_c": 32.4}`, signed(deviceKey, body), "application/json", 401, "invalid_signature"},
		{"not JSON", id, body, signed(deviceKey, body), "text/plain", 415, "unsupported_media_type"},
		{"unknown station", "999", body, signed(deviceKey, body), "application/json", 404, "not_found"},
		{"invalid id", "abc", body, signed(deviceKey, body), "application/json", 400, "invalid_parameter"},
		{"out of range", id, `{"temperature_c": 99}`, signed(deviceKey, `{"temperature_c": 99}`), "application/json", 400, "invalid_parameter"},
	}
	for _, c := range cases {
		r := s.device(c.id, c.body, c.signature, c.contentType)
		if r.status != c.status {
			t.Errorf("%s: status %d, want %d (%s)", c.name, r.status, c.status, r.body)
			continue
		}
		if code, _ := errorField(t, r.body); code != c.code {
			t.Errorf("%s: code %q, want %q", c.name, code, c.code)
		}
	}

	big := `{"temperature_c": 22.4, "x": "` + strings.Repeat("a", 20<<10) + `"}`
	if r := s.device(id, big, signed(deviceKey, big), "application/json"); r.status != http.StatusRequestEntityTooLarge {
		t.Errorf("20 KiB body: status %d, want 413", r.status)
	}
	if got := len(s.stations.readings); got != 0 {
		t.Errorf("a refused submission was stored (%d)", got)
	}
}

func TestStationWithoutKeyDoesNotReceive(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	id := itoa(dataID(t, s.api(http.MethodPost, "/api/stations", stationJSON).body))
	body := `{"temperature_c": 22.4}`
	r := s.device(id, body, signed(deviceKey, body), "application/json")
	if r.status != http.StatusForbidden {
		t.Errorf("status %d, want 403", r.status)
	}
	if code, _ := errorField(t, r.body); code != "uplink_disabled" {
		t.Errorf("code %q", code)
	}
}

func TestPublicKeyBelongsToOneStation(t *testing.T) {
	s := newSite(t)
	stationWithKey(t, s)
	payload, _ := json.Marshal(map[string]any{
		"external_code": "sitio-02", "name": "Outro", "lat": -23.5, "lon": -46.6,
		"public_key": deviceKeyLine(deviceKey), "report_interval_s": 600,
	})
	r := s.api(http.MethodPost, "/api/stations", string(payload))
	if r.status != http.StatusConflict {
		t.Fatalf("status %d, want 409 (%s)", r.status, r.body)
	}
	if code, field := errorField(t, r.body); code != "key_taken" || field != "public_key" {
		t.Errorf("error %q on %q", code, field)
	}
}

func TestPanelFormSetsTheKey(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	form := stationForm("Sítio", "sitio-01", "-23,5", "-46,6")
	form.Set("public_key", "ssh-rsa AAAAB3NzaC1yc2E teste")
	form.Set("report_interval_s", "600")

	refused := s.post("/painel/estacoes", form, false)
	if refused.status != http.StatusBadRequest || !strings.Contains(refused.body, "ssh-keygen -t ed25519") {
		t.Fatalf("RSA key: status %d", refused.status)
	}
	if !strings.Contains(refused.body, `id="public_key"`) || !strings.Contains(refused.body, `aria-invalid="true"`) {
		t.Error("the key field is not marked")
	}

	form.Set("public_key", deviceKeyLine(deviceKey))
	if r := s.post("/painel/estacoes", form, false); r.status != http.StatusSeeOther {
		t.Fatalf("valid key: status %d", r.status)
	}
	edit := s.get("/painel/estacoes/1/editar", false)
	for _, piece := range []string{"SHA256:", "/api/stations/1/readings", "aguardando o 1º envio", `<option value="600" selected>`} {
		if !strings.Contains(edit.body, piece) {
			t.Errorf("edit page lacks %q", piece)
		}
	}
}
