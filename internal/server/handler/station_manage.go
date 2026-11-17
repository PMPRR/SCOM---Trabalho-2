package handler

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// The station panel, /painel/estacoes, is where a logged-in user registers,
// changes, turns off and deletes stations. Every route is behind
// RequireAuth; which stations each user may touch is decided by the service.
// The forms work with HTMX (only a piece of the page changes) and without
// JavaScript (plain posts that come back as whole pages).

const (
	panelPath        = "/painel/estacoes"
	panelRowsPerPage = 25
)

// panelNotices are the messages a redirect back to the panel may ask for,
// by name, in ?aviso=. Only these can appear: the query never carries text.
var panelNotices = map[string]string{
	"criada":     "Estação cadastrada.",
	"alterada":   "Estação alterada.",
	"excluida":   "Estação excluída.",
	"ativada":    "Estação ativada: ela volta a aparecer no mapa.",
	"desativada": "Estação desativada: ela sai do mapa e da lista pública.",
}

// Panel handles GET /painel/estacoes?q=&offset=
func (h *StationHandler) Panel(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	offset, ok := queryInt(r, "offset", 0)
	if !ok {
		h.render.BadRequest(w, r, "offset", "O parâmetro offset deve ser um número inteiro.")
		return
	}
	search := r.URL.Query().Get("q")
	stations, page, err := h.stations.ListManaged(r.Context(), actor, search, panelRowsPerPage, offset)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}

	data := newPageData(r, "")
	view := panelView{
		IsAdmin: actor.IsAdmin(),
		Search:  strings.TrimSpace(search),
		Notice:  panelNotices[r.URL.Query().Get("aviso")],
		Rows:    newPanelRows(stations, page, search, data.CSRFToken),
	}
	if view.IsAdmin {
		view.Heading = "Estações"
	} else {
		view.Heading = "Minhas estações"
	}
	data.Data = view
	h.render.HTML(w, r, http.StatusOK, "stations_panel", data)
}

// PanelRows handles GET /painel/estacoes/linhas?q=&offset=, the rows of the
// table as an HTMX fragment: a search replaces the whole body of the table
// (offset 0), "load more" replaces its own row with the next page.
func (h *StationHandler) PanelRows(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	offset, ok := queryInt(r, "offset", 0)
	if !ok {
		h.render.BadRequest(w, r, "offset", "O parâmetro offset deve ser um número inteiro.")
		return
	}
	search := r.URL.Query().Get("q")
	stations, page, err := h.stations.ListManaged(r.Context(), actor, search, panelRowsPerPage, offset)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	rows := newPanelRows(stations, page, search, newPageData(r, "").CSRFToken)
	if offset == 0 {
		// A new search: the status line above the table changes too, and the
		// address bar keeps the search, so a reload shows the same list.
		rows.StatusOOB = true
		w.Header().Set("HX-Replace-Url", panelURL(search, 0))
	}
	h.render.Fragment(w, r, http.StatusOK, "panel_rows", rows)
}

// NewStationForm handles GET /painel/estacoes/nova
func (h *StationHandler) NewStationForm(w http.ResponseWriter, r *http.Request) {
	data := newPageData(r, "")
	data.Form = map[string]string{"active": "on"}
	data.Data = newStationFormView(r, nil)
	h.render.HTML(w, r, http.StatusOK, "station_form", data)
}

// CreateFromForm handles POST /painel/estacoes
func (h *StationHandler) CreateFromForm(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	in, typed, err := stationInputFromForm(r)
	if err == nil {
		_, err = h.stations.Create(r.Context(), actor, in)
	}
	if err != nil {
		data := newPageData(r, "")
		data.Form = typed
		data.Data = newStationFormView(r, nil)
		showFormError(h.render, w, r, "station_form", data, err)
		return
	}
	redirectTo(w, r, panelPath+"?aviso=criada")
}

// EditStationForm handles GET /painel/estacoes/{id}/editar
func (h *StationHandler) EditStationForm(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	station, err := h.stations.GetManaged(r.Context(), actor, id)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	data := newPageData(r, "")
	data.Form = stationFormValues(station)
	data.Data = newStationFormView(r, &station)
	h.render.HTML(w, r, http.StatusOK, "station_form", data)
}

// UpdateFromForm handles PUT /painel/estacoes/{id} (HTMX) and POST
// /painel/estacoes/{id} (the same form without JavaScript).
func (h *StationHandler) UpdateFromForm(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	in, typed, err := stationInputFromForm(r)
	if err == nil {
		_, err = h.stations.Update(r.Context(), actor, id, in)
	}
	if err != nil {
		// The page needs the station to be redrawn; when even that fails
		// (gone, or not this user's), the error is about the station itself.
		station, getErr := h.stations.GetManaged(r.Context(), actor, id)
		if getErr != nil {
			h.render.Fail(w, r, getErr)
			return
		}
		data := newPageData(r, "")
		data.Form = typed
		data.Data = newStationFormView(r, &station)
		showFormError(h.render, w, r, "station_form", data, err)
		return
	}
	redirectTo(w, r, panelPath+"?aviso=alterada")
}

// SetActiveFromForm handles POST /painel/estacoes/{id}/ativa, field
// active=true|false. HTMX gets the row of the table again, changed.
func (h *StationHandler) SetActiveFromForm(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	active, err := strconv.ParseBool(r.PostFormValue("active"))
	if err != nil {
		h.render.BadRequest(w, r, "active", "O campo active deve ser true ou false.")
		return
	}
	station, err := h.stations.SetActive(r.Context(), actor, id, active)
	if err != nil {
		h.render.Fail(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.render.Fragment(w, r, http.StatusOK, "panel_row", newPanelRow(station, newPageData(r, "").CSRFToken))
		return
	}
	notice := "desativada"
	if active {
		notice = "ativada"
	}
	redirectTo(w, r, panelPath+"?aviso="+notice)
}

// DeleteFromForm handles DELETE /painel/estacoes/{id} (HTMX) and POST
// /painel/estacoes/{id}/excluir (without JavaScript). HTMX gets an empty
// 200, which takes the place of the row: the row disappears.
func (h *StationHandler) DeleteFromForm(w http.ResponseWriter, r *http.Request) {
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
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	redirectTo(w, r, panelPath+"?aviso=excluida")
}

// stationInputFromForm reads the station form. It returns what was typed as
// well, so a refused form comes back filled in. Numbers may use a comma as
// the decimal separator, as people write them in Brazil.
func stationInputFromForm(r *http.Request) (domain.StationInput, map[string]string, error) {
	typed := map[string]string{
		"name":              r.PostFormValue("name"),
		"external_code":     r.PostFormValue("external_code"),
		"lat":               r.PostFormValue("lat"),
		"lon":               r.PostFormValue("lon"),
		"altitude_m":        r.PostFormValue("altitude_m"),
		"active":            r.PostFormValue("active"),
		"public_key":        r.PostFormValue("public_key"),
		"report_interval_s": r.PostFormValue("report_interval_s"),
	}
	in := domain.StationInput{
		Name:         typed["name"],
		ExternalCode: typed["external_code"],
		Active:       typed["active"] != "",
		PublicKey:    typed["public_key"],
	}
	if raw := strings.TrimSpace(typed["report_interval_s"]); raw != "" {
		seconds, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return in, typed, &domain.ValidationError{Field: "report_interval_s", Message: "Escolha um intervalo de envio da lista."}
		}
		in.ReportIntervalS = int32(seconds)
	}

	var ok bool
	if in.Lat, ok = parseDecimal(typed["lat"]); !ok {
		return in, typed, &domain.ValidationError{Field: "lat", Message: "A latitude deve ser um número, como -23,55."}
	}
	if in.Lon, ok = parseDecimal(typed["lon"]); !ok {
		return in, typed, &domain.ValidationError{Field: "lon", Message: "A longitude deve ser um número, como -46,63."}
	}
	if strings.TrimSpace(typed["altitude_m"]) != "" {
		altitude, ok := parseDecimal(typed["altitude_m"])
		if !ok {
			return in, typed, &domain.ValidationError{Field: "altitude_m", Message: "A altitude deve ser um número de metros, como 760."}
		}
		a := float32(altitude)
		in.AltitudeM = &a
	}
	return in, typed, nil
}

// parseDecimal reads a finite number written with a point or a comma.
func parseDecimal(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.Replace(raw, ",", ".", 1))
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// formatDecimal writes a number the way the form reads it back.
func formatDecimal(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1)
}

func stationFormValues(st domain.Station) map[string]string {
	values := map[string]string{
		"name":          st.Name,
		"external_code": st.ExternalCode,
		"lat":           formatDecimal(st.Lat),
		"lon":           formatDecimal(st.Lon),
	}
	if st.AltitudeM != nil {
		values["altitude_m"] = formatDecimal(float64(*st.AltitudeM))
	}
	if st.Active {
		values["active"] = "on"
	}
	values["public_key"] = st.PublicKey
	if st.ReportIntervalS != nil {
		values["report_interval_s"] = strconv.Itoa(int(*st.ReportIntervalS))
	}
	return values
}

// reportIntervals are the choices of the form, in seconds. The API accepts
// any value from 60 to 86400.
var reportIntervals = []struct {
	Seconds int
	Label   string
}{
	{60, "1 minuto"}, {300, "5 minutos"}, {600, "10 minutos"}, {900, "15 minutos"},
	{1800, "30 minutos"}, {3600, "1 hora"}, {10800, "3 horas"}, {21600, "6 horas"},
	{86400, "24 horas"},
}

var uplinkLabels = map[domain.UplinkStatus]string{
	domain.UplinkWaiting: "aguardando o 1º envio",
	domain.UplinkOK:      "envio em dia",
	domain.UplinkLate:    "envio atrasado",
}

func panelURL(search string, offset int) string {
	query := url.Values{}
	if s := strings.TrimSpace(search); s != "" {
		query.Set("q", s)
	}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	if len(query) == 0 {
		return panelPath
	}
	return panelPath + "?" + query.Encode()
}

// panelView feeds the "stations_panel" page.
type panelView struct {
	Heading string
	IsAdmin bool
	Search  string
	Notice  string
	Rows    panelRowsView
}

// panelRowsView feeds the "panel_rows" fragment: the rows of one page.
type panelRowsView struct {
	Rows      []panelRow
	Status    string
	StatusOOB bool   // also replace the status line above the table
	Empty     string // shown in the table when nothing matches
	MoreURL   string // next page, as a fragment for HTMX
	MorePage  string // next page, as a whole page without JavaScript
}

// panelRow is one station in the table of the panel.
type panelRow struct {
	DOMID       string
	Name        string
	Code        string
	Institute   string
	Official    bool
	Active      bool
	Reading     string
	Uplink      string // "", or the state of the station's own submissions
	UplinkLate  bool
	EditURL     string
	ActiveURL   string
	DeleteURL   string
	NextActive  string // value the on/off button sends
	ToggleLabel string
	CSRFToken   string // the row's forms also work without JavaScript
}

func newPanelRows(stations []domain.Station, page service.Page, search, csrf string) panelRowsView {
	view := panelRowsView{Rows: make([]panelRow, 0, len(stations))}
	for _, st := range stations {
		view.Rows = append(view.Rows, newPanelRow(st, csrf))
	}

	search = strings.TrimSpace(search)
	switch {
	case page.Total == 0 && search != "":
		view.Status = fmt.Sprintf("Nenhuma estação encontrada para “%s”.", search)
		view.Empty = "Nenhuma estação encontrada. Tente outra parte do nome ou do código."
	case page.Total == 0:
		view.Status = "Nenhuma estação cadastrada ainda."
		view.Empty = "Você ainda não cadastrou estações."
	case page.Total == 1:
		view.Status = "1 estação."
	default:
		view.Status = fmt.Sprintf("%d estações.", page.Total)
	}

	if next := page.Offset + len(stations); len(stations) > 0 && next < page.Total {
		query := url.Values{"offset": {strconv.Itoa(next)}}
		if search != "" {
			query.Set("q", search)
		}
		view.MoreURL = panelPath + "/linhas?" + query.Encode()
		view.MorePage = panelURL(search, next)
	}
	return view
}

func newPanelRow(st domain.Station, csrf string) panelRow {
	base := fmt.Sprintf("%s/%d", panelPath, st.ID)
	row := panelRow{
		DOMID:     fmt.Sprintf("linha-estacao-%d", st.ID),
		Name:      st.Name,
		Code:      st.ExternalCode,
		Institute: st.InstituteName,
		Official:  st.IsOfficial(),
		Active:    st.Active,
		Reading:   "sem leitura",
		EditURL:   base + "/editar",
		ActiveURL: base + "/ativa",
		DeleteURL: base,
		CSRFToken: csrf,
	}
	if st.Active {
		row.NextActive, row.ToggleLabel = "false", "Desativar"
	} else {
		row.NextActive, row.ToggleLabel = "true", "Ativar"
	}
	if st.Latest != nil {
		row.Reading = st.Latest.ObservedAt.In(saoPaulo).Format("02/01 às 15:04")
	}
	row.Uplink = uplinkLabels[st.UplinkStatus]
	row.UplinkLate = st.UplinkStatus == domain.UplinkLate
	return row
}

// stationFormView feeds the "station_form" page, for a new station (nil)
// or for an existing one.
type stationFormView struct {
	Intervals []struct {
		Seconds int
		Label   string
	}
	// Sending, for a station that already has a key.
	HasKey      bool
	Fingerprint string
	SubmitURL   string
	Uplink      string
	UplinkLate  bool
	LastReport  string
	New         bool
	Heading     string
	Action      string
	Official    bool
	Institute   string
	Name        string
	ActiveURL   string
	Active      bool
	NextActive  string
	ToggleLabel string
}

func newStationFormView(r *http.Request, st *domain.Station) stationFormView {
	if st == nil {
		return stationFormView{New: true, Heading: "Nova estação", Action: panelPath, Intervals: reportIntervals}
	}
	row := newPanelRow(*st, "")
	view := stationFormView{
		Intervals:   reportIntervals,
		HasKey:      st.PublicKey != "",
		Fingerprint: st.KeyFingerprint,
		SubmitURL:   submitURL(r, st.ID),
		Uplink:      row.Uplink,
		UplinkLate:  row.UplinkLate,
		LastReport:  "nenhum",
		Heading:     "Editar estação",
		Action:      fmt.Sprintf("%s/%d", panelPath, st.ID),
		Official:    st.IsOfficial(),
		Institute:   st.InstituteName,
		Name:        st.Name,
		ActiveURL:   row.ActiveURL,
		Active:      st.Active,
		NextActive:  row.NextActive,
		ToggleLabel: row.ToggleLabel,
	}
	if st.LastReportAt != nil {
		view.LastReport = st.LastReportAt.In(saoPaulo).Format("02/01/2006 às 15:04")
	}
	return view
}

// submitURL is the address the station sends its readings to, as the
// browser reached the server. Behind a proxy that rewrites the host it may
// differ; the path is what matters.
func submitURL(r *http.Request, id int64) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/stations/%d/readings", scheme, r.Host, id)
}
