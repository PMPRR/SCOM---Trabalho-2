// Package handler translates HTTP requests into service calls and service
// results into HTTP responses. It holds no business rule.
package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// StationService is what the handlers need from the business layer.
type StationService interface {
	List(ctx context.Context, limit, offset int) ([]domain.Station, service.Page, error)
	Get(ctx context.Context, id int64) (domain.Station, error)
}

type StationHandler struct {
	stations StationService
	render   *render.Renderer
}

func NewStationHandler(stations StationService, rd *render.Renderer) *StationHandler {
	return &StationHandler{stations: stations, render: rd}
}

// List handles GET /api/stations?limit=&offset=
func (h *StationHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(r, "limit", service.DefaultPageLimit)
	if !ok {
		h.render.BadRequest(w, r, "limit", "O parâmetro limit deve ser um número inteiro.")
		return
	}
	offset, ok := queryInt(r, "offset", 0)
	if !ok {
		h.render.BadRequest(w, r, "offset", "O parâmetro offset deve ser um número inteiro.")
		return
	}

	stations, page, err := h.stations.List(r.Context(), limit, offset)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.render.JSON(w, http.StatusOK, map[string]any{"data": stations, "pagination": page})
}

// Get handles GET /api/stations/{id}
func (h *StationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.render.BadRequest(w, r, "id", "O id da estação deve ser um número inteiro.")
		return
	}

	station, err := h.stations.Get(r.Context(), id)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.render.JSON(w, http.StatusOK, map[string]any{"data": station})
}

// cardsPerPage is how many station cards one request of the list returns.
const cardsPerPage = 12

// Cards handles GET /estacoes?offset= and answers with an HTML fragment for
// HTMX. The first page carries the whole list (status line and grid); the
// following ones carry only the cards, which replace the "load more" button.
func (h *StationHandler) Cards(w http.ResponseWriter, r *http.Request) {
	offset, ok := queryInt(r, "offset", 0)
	if !ok {
		h.render.BadRequest(w, r, "offset", "O parâmetro offset deve ser um número inteiro.")
		return
	}

	stations, page, err := h.stations.List(r.Context(), cardsPerPage, offset)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	fragment := "station_list"
	if offset > 0 {
		fragment = "station_items"
	}
	h.render.Fragment(w, r, http.StatusOK, fragment, newStationListView(stations, page))
}

// Card handles GET /estacoes/{id}: the card of one station, which the map
// shows in a popup when the station is selected.
func (h *StationHandler) Card(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.render.BadRequest(w, r, "id", "O id da estação deve ser um número inteiro.")
		return
	}

	station, err := h.stations.Get(r.Context(), id)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.render.Fragment(w, r, http.StatusOK, "station_detail", newStationCard(station, false))
}

// queryInt reads an optional integer query parameter. It reports false when
// the parameter is present but is not an integer.
func queryInt(r *http.Request, name string, fallback int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

