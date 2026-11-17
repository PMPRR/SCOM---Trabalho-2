package handler

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// newPageServer renders the real templates of web/templates, so a broken
// template fails the test suite instead of the first visitor.
func newPageServer(t *testing.T) http.Handler {
	t.Helper()
	rd := render.New(slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err := rd.LoadTemplates(os.DirFS("../../../web/templates")); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	h := NewPageHandler(rd)
	auth := NewAuthHandler(nil, rd, false) // the form pages never reach the service

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.Home)
	mux.HandleFunc("GET /mapa", h.Map)
	mux.HandleFunc("GET /entrar", auth.LoginPage)
	mux.HandleFunc("GET /cadastro", auth.SignupPage)
	return mux
}

func TestPagesRender(t *testing.T) {
	srv := newPageServer(t)

	cases := []struct {
		url   string
		title string
		want  string // a piece of markup only this page has
	}{
		{"/", "<title>ClimaSP - Agregador Metereológico</title>", `id="hero-heading"`},
		{"/mapa", "<title>Mapa interativo - ClimaSP</title>", `id="map"`},
		{"/entrar", "<title>Entrar - ClimaSP</title>", `id="login-heading"`},
		{"/cadastro", "<title>Criar conta - ClimaSP</title>", `id="signup-heading"`},
	}
	for _, c := range cases {
		rec := get(t, srv, c.url)
		body := rec.Body.String()

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", c.url, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q", c.url, ct)
		}
		for _, piece := range []string{c.title, c.want, `class="site-header"`, `class="site-footer"`} {
			if !strings.Contains(body, piece) {
				t.Errorf("%s: response lacks %q", c.url, piece)
			}
		}
		if n := strings.Count(body, "<h1"); n != 1 {
			t.Errorf("%s: %d h1 elements, want exactly 1", c.url, n)
		}
	}
}

func TestNavigationMarksCurrentPage(t *testing.T) {
	srv := newPageServer(t)

	cases := []struct {
		url  string
		want string // empty: no entry of the navigation is current
	}{
		{"/", `<a href="/" aria-current="page">`},
		{"/mapa", `<a href="/mapa" aria-current="page">`},
		{"/entrar", ""},
	}
	for _, c := range cases {
		body := get(t, srv, c.url).Body.String()

		if n := strings.Count(body, `aria-current="page"`); c.want == "" && n != 0 {
			t.Errorf("%s: %d current entries, want none", c.url, n)
		} else if c.want != "" && (n != 1 || !strings.Contains(body, c.want)) {
			t.Errorf("%s: want exactly %q marked as current (found %d)", c.url, c.want, n)
		}
	}
}

func TestUnknownPageIs404(t *testing.T) {
	if rec := get(t, newPageServer(t), "/nao-existe"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
