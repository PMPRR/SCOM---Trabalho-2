package handler

import (
	"net/http"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// PageData is what every page template receives.
type PageData struct {
	// Active names the entry of the main navigation that gets
	// aria-current="page": "home", "map" or empty.
	Active string
	// User is the logged-in user, or nil for a visitor.
	User *domain.User
	// CSRFToken goes into the hidden field of every form and into the
	// header HTMX sends (see middleware.CSRF).
	CSRFToken string
	// FormError is the message shown above a form that was refused, and
	// FormField the input it is about (empty when it is about the whole
	// form). The page marks that input aria-invalid and puts the focus there.
	FormError string
	FormField string
	// Form carries what the user had typed, so a refused form comes back
	// filled in. It never carries passwords.
	Form map[string]string
	// Data is what the page itself shows.
	Data any
}

// newPageData fills in what every page needs from the request.
func newPageData(r *http.Request, active string) PageData {
	data := PageData{Active: active, CSRFToken: middleware.CSRFToken(r.Context())}
	if user, ok := middleware.UserFrom(r.Context()); ok {
		data.User = &user
	}
	return data
}

// PageHandler serves the HTML pages of the site that are open to everyone.
type PageHandler struct {
	render *render.Renderer
}

func NewPageHandler(rd *render.Renderer) *PageHandler {
	return &PageHandler{render: rd}
}

// Home handles GET /
func (h *PageHandler) Home(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "home", newPageData(r, "home"))
}

// Map handles GET /mapa
func (h *PageHandler) Map(w http.ResponseWriter, r *http.Request) {
	h.render.HTML(w, r, http.StatusOK, "map", newPageData(r, "map"))
}

// errorView is what the "error" page shows.
type errorView struct {
	Status  int
	Message string
}

// ErrorPageData builds the data of the "error" page. It is handed to the
// renderer (Renderer.UseErrorPage), so that someone browsing gets a page of
// the site, with its header and the right account menu, instead of JSON.
func ErrorPageData(r *http.Request, status int, message string) any {
	data := newPageData(r, "")
	data.Data = errorView{Status: status, Message: message}
	return data
}
