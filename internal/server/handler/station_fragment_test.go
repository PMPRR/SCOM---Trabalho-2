package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// newFragmentServer serves the HTMX fragments from the real templates.
func newFragmentServer(t *testing.T, repo *fakeRepo) http.Handler {
	t.Helper()
	rd := render.New(slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err := rd.LoadTemplates(os.DirFS("../../../web/templates")); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	h := NewStationHandler(service.NewStationService(repo, slog.New(slog.NewTextHandler(io.Discard, nil))), rd)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /estacoes", h.Cards)
	mux.HandleFunc("GET /estacoes/{id}", h.Card)
	return mux
}

// getHTMX sends the request the way HTMX does.
func getHTMX(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func f32(v float32) *float32 { return &v }
func i16(v int16) *int16     { return &v }

// manyStations returns n stations with a fresh reading each.
func manyStations(n int) *fakeRepo {
	repo := &fakeRepo{}
	for i := 1; i <= n; i++ {
		repo.stations = append(repo.stations, domain.Station{
			ID: int64(i), Name: fmt.Sprintf("Estação %d", i), InstituteName: "INMET", InstituteCode: "inmet", Active: true,
			Latest: &domain.Observation{ObservedAt: time.Now().Add(-10 * time.Minute), TemperatureC: f32(20)},
		})
	}
	return repo
}

func TestCardsFirstPageIsTheWholeList(t *testing.T) {
	rec := getHTMX(t, newFragmentServer(t, manyStations(30)), "/estacoes")
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if strings.Contains(body, "<html") {
		t.Error("a fragment must not carry the page layout")
	}
	if n := strings.Count(body, `class="station-card"`); n != cardsPerPage {
		t.Errorf("%d cards, want %d", n, cardsPerPage)
	}
	for _, piece := range []string{"30 estações cadastradas.", `id="station-grid"`, `hx-get="/estacoes?offset=12"`, `id="estacao-1"`} {
		if !strings.Contains(body, piece) {
			t.Errorf("fragment lacks %q", piece)
		}
	}
}

func TestCardsNextPagesCarryOnlyItems(t *testing.T) {
	srv := newFragmentServer(t, manyStations(30))

	second := getHTMX(t, srv, "/estacoes?offset=12").Body.String()
	if strings.Contains(second, `id="station-grid"`) || strings.Contains(second, "estações cadastradas") {
		t.Error("a following page must carry only the cards, not the list wrapper")
	}
	if !strings.Contains(second, `id="estacao-13"`) || !strings.Contains(second, `hx-get="/estacoes?offset=24"`) {
		t.Errorf("second page = %s", second)
	}

	last := getHTMX(t, srv, "/estacoes?offset=24").Body.String()
	if n := strings.Count(last, `class="station-card"`); n != 6 {
		t.Errorf("%d cards on the last page, want 6", n)
	}
	if strings.Contains(last, "hx-get") {
		t.Error("the last page must not offer more")
	}
}

func TestCardsEmptyDatabase(t *testing.T) {
	body := getHTMX(t, newFragmentServer(t, &fakeRepo{}), "/estacoes").Body.String()

	if !strings.Contains(body, "Nenhuma estação cadastrada ainda.") || strings.Contains(body, "hx-get") {
		t.Errorf("body = %s", body)
	}
}

func TestCardFormatsTheReading(t *testing.T) {
	owner := int64(3)
	repo := &fakeRepo{stations: []domain.Station{{
		ID: 7, Name: "Sorocaba - Campus", InstituteName: "Estação privada", InstituteCode: "privada", OwnerID: &owner, Active: true,
		Latest: &domain.Observation{
			ObservedAt:   time.Now().Add(-20 * time.Minute),
			TemperatureC: f32(24.13), WindSpeedMS: f32(3.33), WindDirDeg: i16(160),
		},
	}}}

	rec := getHTMX(t, newFragmentServer(t, repo), "/estacoes/7")
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for _, piece := range []string{
		"Sorocaba - Campus",
		"Estação privada &middot; confiança média",
		"<dd>24.1 °C</dd>",
		"<dd>12 km/h de sul</dd>", // 3.33 m/s; 160 degrees rounds to south
		"<dd>sem dado</dd>",       // no rain sensor
	} {
		if !strings.Contains(body, piece) {
			t.Errorf("card lacks %q\n%s", piece, body)
		}
	}
	if strings.Contains(body, `id="estacao-7"`) {
		t.Error("the popup card must not repeat the id of the card in the list")
	}
}

func TestCardWithoutReading(t *testing.T) {
	repo := &fakeRepo{stations: []domain.Station{{ID: 1, Name: "Nova", InstituteName: "INMET", Active: true}}}

	body := getHTMX(t, newFragmentServer(t, repo), "/estacoes/1").Body.String()

	if !strings.Contains(body, "Ainda sem leitura.") || strings.Contains(body, "confiança") {
		t.Errorf("body = %s", body)
	}
}

func TestCardEscapesStationName(t *testing.T) {
	repo := &fakeRepo{stations: []domain.Station{{ID: 1, Name: `<script>alert(1)</script>`, InstituteName: `"><img src=x onerror=alert(1)>`, Active: true}}}

	body := getHTMX(t, newFragmentServer(t, repo), "/estacoes/1").Body.String()

	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") {
		t.Errorf("station text reached the page as HTML: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("escaped name not found: %s", body)
	}
}

func TestFragmentErrorsAreHTMLForHTMX(t *testing.T) {
	srv := newFragmentServer(t, manyStations(3))

	cases := []struct {
		url    string
		status int
		text   string
	}{
		{"/estacoes/99", http.StatusNotFound, "Recurso não encontrado."},
		{"/estacoes/abc", http.StatusBadRequest, "O id da estação deve ser um número inteiro."},
		{"/estacoes?offset=-1", http.StatusBadRequest, "O deslocamento não pode ser negativo."},
	}
	for _, c := range cases {
		rec := getHTMX(t, srv, c.url)
		if rec.Code != c.status {
			t.Errorf("%s: status = %d, want %d", c.url, rec.Code, c.status)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `class="request-error" role="alert"`) || !strings.Contains(body, c.text) {
			t.Errorf("%s: body = %s", c.url, body)
		}
	}

	// The same failure, asked without HTMX, keeps the JSON format of the API.
	plain := get(t, srv, "/estacoes/99")
	if !strings.Contains(plain.Body.String(), `"code":"not_found"`) {
		t.Errorf("non-HTMX error = %s", plain.Body.String())
	}
}

func TestFragmentInternalErrorHidesCause(t *testing.T) {
	repo := &fakeRepo{err: errors.New("connection refused to 10.0.0.5")}

	rec := getHTMX(t, newFragmentServer(t, repo), "/estacoes")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("response leaks the cause: %s", rec.Body.String())
	}
}
