package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// These tests run the station panel and the write API over HTTP, through
// the real middlewares, service and templates (see newSite).

// send makes a request that changes something. withToken puts the CSRF
// token in the header, as HTMX and API clients do.
func (s *site) send(method, path, contentType, body string, htmx, withToken bool) reply {
	s.t.Helper()
	token := s.csrf()
	req, _ := http.NewRequest(method, s.url+path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if withToken {
		req.Header.Set("X-CSRF-Token", token)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return s.do(req)
}

func (s *site) api(method, path, body string) reply {
	s.t.Helper()
	return s.send(method, path, "application/json", body, false, true)
}

// seedOfficial puts a station collected from an institute in the store.
func (s *site) seedOfficial() {
	s.stations.stations = append(s.stations.stations, domain.Station{
		ID: 500, Name: "Pluviômetro oficial", ExternalCode: "355030801A",
		InstituteCode: "cemaden", InstituteName: "CEMADEN", Active: true,
	})
}

const stationJSON = `{"external_code":"sitio-01","name":"Sítio da Ana","lat":-23.55,"lon":-46.63,"altitude_m":760}`

func stationForm(name, code, lat, lon string) url.Values {
	return url.Values{"name": {name}, "external_code": {code}, "lat": {lat}, "lon": {lon}, "active": {"on"}}
}

func dataID(t *testing.T, body string) int64 {
	t.Helper()
	var got struct {
		Data domain.Station `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Data.ID
}

func errorField(t *testing.T, body string) (code, field string) {
	t.Helper()
	var got struct {
		Error struct {
			Code  string `json:"code"`
			Field string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Error.Code, got.Error.Field
}

func TestStationWritesNeedLogin(t *testing.T) {
	s := newSite(t)
	s.seedOfficial()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/stations"},
		{http.MethodPut, "/api/stations/500"},
		{http.MethodPatch, "/api/stations/500"},
		{http.MethodDelete, "/api/stations/500"},
	} {
		r := s.api(c.method, c.path, stationJSON)
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s %s without login: status %d, want 401", c.method, c.path, r.status)
		}
	}
	if r := s.get("/painel/estacoes", false); r.status != http.StatusSeeOther || r.header.Get("Location") != "/entrar" {
		t.Errorf("panel without login: status %d to %q, want 303 to /entrar", r.status, r.header.Get("Location"))
	}
	if len(s.stations.stations) != 1 {
		t.Error("an anonymous request changed the stations")
	}
}

func TestStationWritesNeedTheCSRFToken(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")

	r := s.send(http.MethodPost, "/api/stations", "application/json", stationJSON, false, false)
	if r.status != http.StatusForbidden {
		t.Errorf("POST without token: status %d, want 403", r.status)
	}
	form := stationForm("Sítio", "sitio-01", "-23,5", "-46,6").Encode()
	r = s.send(http.MethodPost, "/painel/estacoes", "application/x-www-form-urlencoded", form, false, false)
	if r.status != http.StatusForbidden {
		t.Errorf("form without token: status %d, want 403", r.status)
	}
	if len(s.stations.stations) != 0 {
		t.Error("a request without the token created a station")
	}
}

func TestAPIStationLifecycle(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")

	created := s.api(http.MethodPost, "/api/stations", stationJSON)
	if created.status != http.StatusCreated {
		t.Fatalf("POST: status %d, body %s", created.status, created.body)
	}
	id := dataID(t, created.body)
	location := created.header.Get("Location")
	if location != "/api/stations/"+itoa(id) {
		t.Errorf("Location = %q", location)
	}
	if r := s.get(location, false); r.status != http.StatusOK || !strings.Contains(r.body, "Sítio da Ana") {
		t.Errorf("GET the new station: status %d, body %s", r.status, r.body)
	}

	updated := s.api(http.MethodPut, location, `{"external_code":"sitio-01","name":"Sítio novo","lat":-23.6,"lon":-46.7}`)
	if updated.status != http.StatusOK || !strings.Contains(updated.body, `"name":"Sítio novo"`) {
		t.Errorf("PUT: status %d, body %s", updated.status, updated.body)
	}

	off := s.api(http.MethodPatch, location, `{"active":false}`)
	if off.status != http.StatusOK || !strings.Contains(off.body, `"active":false`) {
		t.Errorf("PATCH: status %d, body %s", off.status, off.body)
	}
	if r := s.get(location, false); r.status != http.StatusNotFound {
		t.Errorf("GET of a station turned off: status %d, want 404", r.status)
	}

	if r := s.api(http.MethodDelete, location, ""); r.status != http.StatusNoContent {
		t.Errorf("DELETE: status %d, body %s", r.status, r.body)
	}
	if len(s.stations.stations) != 0 {
		t.Error("the station is still stored")
	}
}

func TestAPIRefusesBadBodies(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")

	cases := []struct {
		name, contentType, body string
		status                  int
		code, field             string
	}{
		{"not JSON", "application/json", `name=x`, 400, "invalid_parameter", "body"},
		{"unknown field", "application/json", `{"nome":"x"}`, 400, "invalid_parameter", "body"},
		{"two objects", "application/json", stationJSON + stationJSON, 400, "invalid_parameter", "body"},
		{"form body", "application/x-www-form-urlencoded", `name=x`, 415, "unsupported_media_type", ""},
		{"no latitude", "application/json", `{"external_code":"a","name":"x","lon":-46}`, 400, "invalid_parameter", "lat"},
		{"outside the State", "application/json", `{"external_code":"a","name":"x","lat":-3.7,"lon":-38.5}`, 400, "invalid_parameter", "lat"},
		{"latitude as text", "application/json", `{"external_code":"a","name":"x","lat":"-23","lon":-46}`, 400, "invalid_parameter", "body"},
	}
	for _, c := range cases {
		r := s.send(http.MethodPost, "/api/stations", c.contentType, c.body, false, true)
		if r.status != c.status {
			t.Errorf("%s: status %d, want %d (%s)", c.name, r.status, c.status, r.body)
			continue
		}
		if code, field := errorField(t, r.body); code != c.code || field != c.field {
			t.Errorf("%s: error %q on %q, want %q on %q", c.name, code, field, c.code, c.field)
		}
	}
	if len(s.stations.stations) != 0 {
		t.Error("a refused body created a station")
	}
}

func TestDuplicateCodeIsAConflict(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	s.api(http.MethodPost, "/api/stations", stationJSON)

	r := s.api(http.MethodPost, "/api/stations", stationJSON)
	if r.status != http.StatusConflict {
		t.Fatalf("status %d, want 409", r.status)
	}
	if code, field := errorField(t, r.body); code != "station_code_taken" || field != "external_code" {
		t.Errorf("error %q on %q", code, field)
	}
}

func TestOperatorCannotChangeAnotherOperatorsStation(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	id := dataID(t, s.api(http.MethodPost, "/api/stations", stationJSON).body)
	path := "/api/stations/" + itoa(id)

	s.post("/sair", url.Values{}, false)
	s.signup("Bruno", "bruno@example.com")

	for _, c := range []struct{ method, body string }{
		{http.MethodPut, stationJSON},
		{http.MethodPatch, `{"active":false}`},
		{http.MethodDelete, ""},
	} {
		if r := s.api(c.method, path, c.body); r.status != http.StatusForbidden {
			t.Errorf("%s by another operator: status %d, want 403", c.method, r.status)
		}
	}
	if r := s.get("/painel/estacoes/"+itoa(id)+"/editar", false); r.status != http.StatusForbidden {
		t.Errorf("edit page of another's station: status %d, want 403", r.status)
	}
	if r := s.get("/painel/estacoes", false); strings.Contains(r.body, "Sítio da Ana") {
		t.Error("Bruno's panel lists Ana's station")
	}
	if st := s.stations.find(id); st == nil || st.Name != "Sítio da Ana" || !st.Active {
		t.Errorf("Ana's station changed: %+v", st)
	}
}

func TestAdminAndOfficialStations(t *testing.T) {
	s := newSite(t)
	s.seedOfficial()
	s.signup("Admin", "admin@example.com")
	s.promote("admin@example.com")

	if r := s.api(http.MethodPut, "/api/stations/500", stationJSON); r.status != http.StatusConflict {
		t.Errorf("PUT official: status %d, want 409", r.status)
	}
	if r := s.api(http.MethodDelete, "/api/stations/500", ""); r.status != http.StatusConflict {
		t.Errorf("DELETE official: status %d, want 409", r.status)
	}
	if r := s.api(http.MethodPatch, "/api/stations/500", `{"active":false}`); r.status != http.StatusOK {
		t.Errorf("PATCH official: status %d, want 200 (%s)", r.status, r.body)
	}

	panel := s.get("/painel/estacoes", false)
	if !strings.Contains(panel.body, "Pluviômetro oficial") || !strings.Contains(panel.body, "Desativada") {
		t.Error("the admin panel does not list the official station, turned off")
	}
	if strings.Contains(panel.body, "/painel/estacoes/500/editar") {
		t.Error("the panel offers to edit an official station")
	}
}

func TestOperatorCannotTurnOffAnOfficialStation(t *testing.T) {
	s := newSite(t)
	s.seedOfficial()
	s.signup("Ana", "ana@example.com")

	if r := s.api(http.MethodPatch, "/api/stations/500", `{"active":false}`); r.status != http.StatusForbidden {
		t.Errorf("status %d, want 403", r.status)
	}
	if !s.stations.find(500).Active {
		t.Error("the official station was turned off")
	}
}

func TestPanelFormWithoutJavaScript(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")

	// A refused form comes back whole, filled in, with the field marked.
	refused := s.post("/painel/estacoes", stationForm("Sítio", "sitio-01", "noventa", "-46,6"), false)
	if refused.status != http.StatusBadRequest {
		t.Fatalf("refused form: status %d, want 400", refused.status)
	}
	for _, piece := range []string{"A latitude deve ser um número", `value="noventa"`, `value="Sítio"`, `aria-invalid="true"`} {
		if !strings.Contains(refused.body, piece) {
			t.Errorf("the refused form lacks %q", piece)
		}
	}

	// Accepted: 303 back to the panel, which lists the station.
	created := s.post("/painel/estacoes", stationForm("Sítio", "sitio-01", "-23,55", "-46,63"), false)
	if created.status != http.StatusSeeOther || created.header.Get("Location") != "/painel/estacoes?aviso=criada" {
		t.Fatalf("form: status %d to %q", created.status, created.header.Get("Location"))
	}
	panel := s.get("/painel/estacoes?aviso=criada", false)
	if !strings.Contains(panel.body, "Estação cadastrada.") || !strings.Contains(panel.body, "sitio-01") {
		t.Error("the panel does not confirm nor list the new station")
	}
	st := s.stations.stations[0]
	if st.Lat != -23.55 || st.Lon != -46.63 {
		t.Errorf("decimal comma read as %v, %v", st.Lat, st.Lon)
	}

	// Edit form shows the values with a decimal comma, and saves.
	path := "/painel/estacoes/" + itoa(st.ID)
	if edit := s.get(path+"/editar", false); !strings.Contains(edit.body, `value="-23,55"`) {
		t.Error("the edit form does not show the latitude")
	}
	saved := s.post(path, stationForm("Sítio novo", "sitio-01", "-23,55", "-46,63"), false)
	if saved.status != http.StatusSeeOther || s.stations.find(st.ID).Name != "Sítio novo" {
		t.Errorf("update form: status %d, name %q", saved.status, s.stations.find(st.ID).Name)
	}

	// Delete without JavaScript.
	if r := s.post(path+"/excluir", url.Values{}, false); r.status != http.StatusSeeOther {
		t.Errorf("delete form: status %d", r.status)
	}
	if len(s.stations.stations) != 0 {
		t.Error("the station was not deleted")
	}
}

func TestPanelWithHTMX(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	form := stationForm("Sítio", "sitio-01", "-23,55", "-46,63")

	created := s.post("/painel/estacoes", form, true)
	if created.status != http.StatusNoContent || created.header.Get("HX-Redirect") != "/painel/estacoes?aviso=criada" {
		t.Fatalf("create: status %d, HX-Redirect %q", created.status, created.header.Get("HX-Redirect"))
	}
	id := s.stations.stations[0].ID
	path := "/painel/estacoes/" + itoa(id)

	// An error is a fragment that names the field.
	bad := s.send(http.MethodPut, path, "application/x-www-form-urlencoded",
		stationForm("", "sitio-01", "-23,55", "-46,63").Encode(), true, true)
	if bad.status != http.StatusBadRequest || !strings.Contains(bad.body, `data-field="name"`) || strings.Contains(bad.body, "<html") {
		t.Errorf("refused PUT: status %d, body %s", bad.status, bad.body)
	}

	// Turning off answers with the new row.
	off := s.post(path+"/ativa", url.Values{"active": {"false"}}, true)
	if off.status != http.StatusOK || !strings.Contains(off.body, `id="linha-estacao-`+itoa(id)+`"`) || !strings.Contains(off.body, "Ativar") {
		t.Errorf("toggle: status %d, body %s", off.status, off.body)
	}

	// A search answers with rows and, out of band, the status line.
	rows := s.get("/painel/estacoes/linhas?q=nada", true)
	if !strings.Contains(rows.body, `hx-swap-oob="true"`) || !strings.Contains(rows.body, "Nenhuma estação encontrada") {
		t.Errorf("search: body %s", rows.body)
	}
	if rows.header.Get("HX-Replace-Url") != "/painel/estacoes?q=nada" {
		t.Errorf("HX-Replace-Url = %q", rows.header.Get("HX-Replace-Url"))
	}

	// Deleting answers with nothing, which removes the row.
	gone := s.send(http.MethodDelete, path, "", "", true, true)
	if gone.status != http.StatusOK || gone.body != "" {
		t.Errorf("delete: status %d, body %q", gone.status, gone.body)
	}
}

func TestStationNameIsEscapedInThePanel(t *testing.T) {
	s := newSite(t)
	s.signup("Ana", "ana@example.com")
	s.api(http.MethodPost, "/api/stations",
		`{"external_code":"xss","name":"<script>alert(1)</script>","lat":-23.5,"lon":-46.6}`)

	for _, path := range []string{"/painel/estacoes", "/painel/estacoes/1/editar"} {
		body := s.get(path, false).body
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Errorf("%s: the name reached the page unescaped", path)
		}
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s: the escaped name is missing", path)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
