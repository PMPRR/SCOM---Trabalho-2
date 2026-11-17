package handler

import (
	"net/http"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// PageData is what every page template receives.
type PageData struct {
	// Active names the entry of the main navigation that gets
	// aria-current="page": "home", "map" or empty.
	Active string
}

// PageHandler serves the HTML pages of the site.
type PageHandler struct {
	render *render.Renderer
}

func NewPageHandler(rd *render.Renderer) *PageHandler {
	return &PageHandler{render: rd}
}

// Home handles GET /
func (h *PageHandler) Home(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "home", PageData{Active: "home"})
}

// Map handles GET /mapa
func (h *PageHandler) Map(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "map", PageData{Active: "map"})
}

// Login handles GET /entrar
func (h *PageHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "login", PageData{})
}

// Signup handles GET /cadastro
func (h *PageHandler) Signup(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "signup", PageData{})
}
