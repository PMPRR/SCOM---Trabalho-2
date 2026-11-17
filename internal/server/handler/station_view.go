package handler

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/service"
)

// The views below hold text ready to print. Units, rounding and wording are
// decided here, in Go, so the templates stay free of logic and the same card
// reads the same wherever it is rendered.

const noData = "sem dado"

// saoPaulo is the time zone readings are shown in. A fixed offset is enough
// (Brazil has had no daylight saving time since 2019) and does not depend on
// the time zone database of the host.
var saoPaulo = time.FixedZone("BRT", -3*60*60)

var compassRose = [8]string{
	"norte", "nordeste", "leste", "sudeste", "sul", "sudoeste", "oeste", "noroeste",
}

var confidenceLabels = map[domain.Confidence]string{
	domain.ConfidenceHigh:   "alta",
	domain.ConfidenceMedium: "média",
	domain.ConfidenceLow:    "baixa",
}

// stationCard is one station as the "station_card" template prints it.
type stationCard struct {
	DOMID       string // id attribute; empty where the card may repeat on the page
	Name        string
	Institute   string
	Confidence  string // "alta", "média", "baixa"; empty without a reading
	HasReading  bool
	Temperature string
	Wind        string
	Rain        string
	UpdatedISO  string // machine-readable, for <time datetime>
	UpdatedText string
}

// stationListView feeds the "station_list" and "station_items" templates.
type stationListView struct {
	Cards   []stationCard
	Total   int
	NextURL string // empty on the last page
}

func newStationListView(stations []domain.Station, page service.Page) stationListView {
	view := stationListView{Cards: make([]stationCard, 0, len(stations)), Total: page.Total}
	for _, st := range stations {
		view.Cards = append(view.Cards, newStationCard(st, true))
	}
	if next := page.Offset + len(stations); len(stations) > 0 && next < page.Total {
		view.NextURL = fmt.Sprintf("/estacoes?offset=%d", next)
	}
	return view
}

// newStationCard formats a station. withID gives the card the anchor
// "estacao-{id}"; it is off for the card shown in the popup of the map,
// which repeats a station that is usually also in the list.
func newStationCard(st domain.Station, withID bool) stationCard {
	card := stationCard{
		Name:       st.Name,
		Institute:  st.InstituteName,
		Confidence: confidenceLabels[st.Confidence],
	}
	if withID {
		card.DOMID = fmt.Sprintf("estacao-%d", st.ID)
	}
	if st.Latest == nil {
		return card
	}

	o := st.Latest
	card.HasReading = true
	card.Temperature = formatValue(o.TemperatureC, "%.1f °C")
	card.Rain = formatValue(o.PrecipitationMM, "%.1f mm")
	card.Wind = formatWind(o.WindSpeedMS, o.WindDirDeg)

	local := o.ObservedAt.In(saoPaulo)
	card.UpdatedISO = local.Format(time.RFC3339)
	card.UpdatedText = local.Format("02/01 às 15:04")
	return card
}

// formatValue writes a reading with a decimal comma, as numbers are written
// in Brazil: "24,3 °C".
func formatValue(v *float32, format string) string {
	if v == nil {
		return noData
	}
	return strings.Replace(fmt.Sprintf(format, *v), ".", ",", 1)
}

// formatWind converts m/s (database) to km/h (screen) and names the
// direction the wind blows from.
func formatWind(speedMS *float32, dirDeg *int16) string {
	if speedMS == nil {
		return noData
	}
	text := fmt.Sprintf("%.0f km/h", math.Round(float64(*speedMS)*3.6))
	if dirDeg == nil {
		return text
	}
	sector := int(math.Round(float64((int(*dirDeg)%360+360)%360)/45)) % 8
	return text + " de " + compassRose[sector]
}
