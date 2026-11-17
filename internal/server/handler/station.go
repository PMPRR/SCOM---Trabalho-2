// Package handler translates HTTP requests into service calls and service
// results into HTTP responses. It holds no business rule.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/middleware"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// StationService is what the handlers need from the business layer. The
// first two are open to everyone; the others act on behalf of a user, and
// the service decides what that user may do.
type StationService interface {
	List(ctx context.Context, limit, offset int) ([]domain.Station, service.Page, error)
	Get(ctx context.Context, id int64) (domain.Station, error)

	ListManaged(ctx context.Context, actor domain.User, search string, limit, offset int) ([]domain.Station, service.Page, error)
	GetManaged(ctx context.Context, actor domain.User, id int64) (domain.Station, error)
	Create(ctx context.Context, actor domain.User, in domain.StationInput) (domain.Station, error)
	Update(ctx context.Context, actor domain.User, id int64, in domain.StationInput) (domain.Station, error)
	SetActive(ctx context.Context, actor domain.User, id int64, active bool) (domain.Station, error)
	Delete(ctx context.Context, actor domain.User, id int64) error

	// Submit receives a reading sent by the station itself, authenticated
	// by its signature and not by a session.
	Submit(ctx context.Context, id int64, payload []byte, signature string) (domain.SubmissionResult, error)
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

// stationRequest is the JSON body of POST and PUT /api/stations. Pointers
// tell a missing field from a zero: a station at latitude 0 is outside the
// State, but a missing latitude is a different mistake.
type stationRequest struct {
	ExternalCode string   `json:"external_code"`
	Name         string   `json:"name"`
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	AltitudeM    *float32 `json:"altitude_m"`
	Active       *bool    `json:"active"` // missing means true
	// Optional: the station sends its own readings. Both or neither.
	PublicKey       string `json:"public_key"`
	ReportIntervalS int32  `json:"report_interval_s"`
}

func (req stationRequest) input() (domain.StationInput, error) {
	if req.Lat == nil {
		return domain.StationInput{}, &domain.ValidationError{Field: "lat", Message: "Informe a latitude."}
	}
	if req.Lon == nil {
		return domain.StationInput{}, &domain.ValidationError{Field: "lon", Message: "Informe a longitude."}
	}
	in := domain.StationInput{
		ExternalCode:    req.ExternalCode,
		Name:            req.Name,
		Lat:             *req.Lat,
		Lon:             *req.Lon,
		AltitudeM:       req.AltitudeM,
		Active:          true,
		PublicKey:       req.PublicKey,
		ReportIntervalS: req.ReportIntervalS,
	}
	if req.Active != nil {
		in.Active = *req.Active
	}
	return in, nil
}

// decodeJSON reads a JSON body into dst. It refuses another content type,
// fields dst does not have and anything after the object, so a typo in a
// field name is an error instead of a value silently ignored. The size of
// the body is already capped by the CSRF middleware.
func (h *StationHandler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mediaType) != "application/json" {
		h.render.Reject(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Envie o corpo em JSON, com o cabeçalho Content-Type: application/json.")
		return false
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil || decoder.More() {
		h.render.BadRequest(w, r, "body",
			"O corpo da requisição não é um JSON válido para esta rota. Confira os nomes e os tipos dos campos.")
		return false
	}
	return true
}

// actor returns the logged-in user. Every route that calls it is behind
// RequireAuth, so a missing user is answered as unauthenticated.
func (h *StationHandler) actor(w http.ResponseWriter, r *http.Request) (domain.User, bool) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		h.render.Fail(w, r, domain.ErrUnauthenticated)
	}
	return user, ok
}

func (h *StationHandler) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.render.BadRequest(w, r, "id", "O id da estação deve ser um número inteiro.")
		return 0, false
	}
	return id, true
}

// Create handles POST /api/stations. It answers 201 with the new station
// and its address in the Location header.
func (h *StationHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req stationRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	station, err := h.stations.Create(r.Context(), actor, in)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/stations/%d", station.ID))
	h.render.JSON(w, http.StatusCreated, map[string]any{"data": station})
}

// Update handles PUT /api/stations/{id}: the body replaces every field a
// user can change.
func (h *StationHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var req stationRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	station, err := h.stations.Update(r.Context(), actor, id, in)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.render.JSON(w, http.StatusOK, map[string]any{"data": station})
}

// Patch handles PATCH /api/stations/{id}, whose only field is "active":
// the one change that official stations accept too.
func (h *StationHandler) Patch(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Active *bool `json:"active"`
	}
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if req.Active == nil {
		h.render.Fail(w, r, &domain.ValidationError{Field: "active", Message: "Informe active: true ou false."})
		return
	}
	station, err := h.stations.SetActive(r.Context(), actor, id, *req.Active)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	h.render.JSON(w, http.StatusOK, map[string]any{"data": station})
}

// Delete handles DELETE /api/stations/{id} and answers 204.
func (h *StationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.stations.Delete(r.Context(), actor, id); err != nil {
		h.render.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SignatureHeader carries the base64 Ed25519 signature of the body of a
// reading sent by a station.
const SignatureHeader = "X-Signature"

// maxReadingBytes caps the body of a reading. One reading is a few hundred
// bytes; the cap keeps a device, or anyone, from making the server read
// more before the signature is checked.
const maxReadingBytes = 16 << 10

// SubmitReading handles POST /api/stations/{id}/readings, the route the
// stations send their readings to. It is outside the CSRF check and the
// session: the request is authenticated by its signature (see
// service.StationService.Submit). It answers 201 for a new reading and 200
// for one already stored, with the earliest time of the next submission.
func (h *StationHandler) SubmitReading(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mediaType) != "application/json" {
		h.render.Reject(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Envie o corpo em JSON, com o cabeçalho Content-Type: application/json.")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReadingBytes))
	if err != nil {
		h.render.Reject(w, r, http.StatusRequestEntityTooLarge, "payload_too_large",
			"Uma leitura deve ter no máximo 16 KiB.")
		return
	}

	result, err := h.stations.Submit(r.Context(), id, payload, r.Header.Get(SignatureHeader))
	var tooSoon *domain.TooSoonError
	if errors.As(err, &tooSoon) {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(tooSoon.Wait.Seconds()))))
	}
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}

	status := http.StatusOK
	if result.Stored {
		status = http.StatusCreated
	}
	h.render.JSON(w, status, map[string]any{"data": map[string]any{
		"stored":      result.Stored,
		"observed_at": result.ObservedAt,
		"next_after":  result.NextAfter,
	}})
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
