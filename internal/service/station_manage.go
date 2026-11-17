package service

import (
	"context"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// Who may change which station:
//
//	                        | operador            | administrador
//	------------------------+---------------------+------------------------
//	registrar estação       | sim (fica dono)     | sim (fica dono)
//	editar / excluir        | só as suas          | as cadastradas no site
//	ativar / desativar      | só as suas          | todas
//
// A station collected from an institute (no owner) is never edited nor
// deleted on the site: the next collection would write its name and
// position back, and would create it again. An administrator can only turn
// it off, which hides it from the public until it is turned on again.

// Limits of a station typed on the site.
const (
	maxSearchLength = 120
	minAltitudeM    = -100 // below sea level, for a sensor in a pit
	maxAltitudeM    = 3000 // above the highest point of the State (2,798 m)
)

// stationCode is the code of a station at its owner: letters, digits and
// . _ -, starting with a letter or digit, up to 50 characters (the limit of
// stations.external_code).
var stationCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,49}$`)

// canManage reports whether actor may change st. Administrators manage
// every station; operators, only the ones they registered.
func canManage(actor domain.User, st domain.Station) bool {
	if actor.IsAdmin() {
		return true
	}
	return st.OwnerID != nil && *st.OwnerID == actor.ID
}

// requireActor refuses an anonymous caller. The routes are already behind
// RequireAuth; this keeps the rule true wherever the service is called from.
func requireActor(actor domain.User) error {
	if actor.ID < 1 {
		return domain.ErrUnauthenticated
	}
	return nil
}

// ListManaged returns one page of the stations actor manages, newest first:
// every station for an administrator, the ones they registered for an
// operator. Stations turned off are included. search narrows the list by
// part of the name or of the code.
func (s *StationService) ListManaged(ctx context.Context, actor domain.User, search string, limit, offset int) ([]domain.Station, Page, error) {
	if err := requireActor(actor); err != nil {
		return nil, Page{}, err
	}
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > maxSearchLength {
		return nil, Page{}, &domain.ValidationError{Field: "q", Message: "A busca pode ter no máximo 120 caracteres."}
	}
	filter := domain.StationFilter{Search: search, NewestFirst: true}
	if !actor.IsAdmin() {
		filter.OwnerID = &actor.ID
	}
	return s.list(ctx, filter, limit, offset)
}

// GetManaged returns a station actor may change, turned on or off.
func (s *StationService) GetManaged(ctx context.Context, actor domain.User, id int64) (domain.Station, error) {
	if err := requireActor(actor); err != nil {
		return domain.Station{}, err
	}
	station, err := s.get(ctx, id)
	if err != nil {
		return domain.Station{}, err
	}
	if !canManage(actor, station) {
		return domain.Station{}, domain.ErrForbidden
	}
	return station, nil
}

// Create registers a station owned by actor.
func (s *StationService) Create(ctx context.Context, actor domain.User, in domain.StationInput) (domain.Station, error) {
	if err := requireActor(actor); err != nil {
		return domain.Station{}, err
	}
	in, err := normalizeStationInput(in)
	if err != nil {
		return domain.Station{}, err
	}
	id, err := s.repo.Create(ctx, actor.ID, in)
	if err != nil {
		return domain.Station{}, err
	}
	s.log.Info("station created", "station_id", id, "user_id", actor.ID)
	return s.get(ctx, id)
}

// Update changes a station registered on the site.
func (s *StationService) Update(ctx context.Context, actor domain.User, id int64, in domain.StationInput) (domain.Station, error) {
	station, err := s.GetManaged(ctx, actor, id)
	if err != nil {
		return domain.Station{}, err
	}
	if station.IsOfficial() {
		return domain.Station{}, domain.ErrOfficialStation
	}
	in, err = normalizeStationInput(in)
	if err != nil {
		return domain.Station{}, err
	}
	if err := s.repo.Update(ctx, id, in); err != nil {
		return domain.Station{}, err
	}
	s.log.Info("station updated", "station_id", id, "user_id", actor.ID)
	return s.get(ctx, id)
}

// SetActive turns a station on or off. It is the one change an official
// station accepts, and only from an administrator.
func (s *StationService) SetActive(ctx context.Context, actor domain.User, id int64, active bool) (domain.Station, error) {
	if _, err := s.GetManaged(ctx, actor, id); err != nil {
		return domain.Station{}, err
	}
	if err := s.repo.SetActive(ctx, id, active); err != nil {
		return domain.Station{}, err
	}
	s.log.Info("station active changed", "station_id", id, "active", active, "user_id", actor.ID)
	return s.get(ctx, id)
}

// Delete removes a station registered on the site, with its readings.
func (s *StationService) Delete(ctx context.Context, actor domain.User, id int64) error {
	station, err := s.GetManaged(ctx, actor, id)
	if err != nil {
		return err
	}
	if station.IsOfficial() {
		return domain.ErrOfficialStation
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.log.Info("station deleted", "station_id", id, "user_id", actor.ID)
	return nil
}

// normalizeStationInput trims what was typed and checks it against the
// rules of a station. The field names are the ones of the API and of the
// form, so the message can be shown next to the right input.
func normalizeStationInput(in domain.StationInput) (domain.StationInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.ExternalCode = strings.TrimSpace(in.ExternalCode)

	switch {
	case in.Name == "":
		return in, &domain.ValidationError{Field: "name", Message: "Informe o nome da estação."}
	case utf8.RuneCountInString(in.Name) > maxStationNameLength:
		return in, &domain.ValidationError{Field: "name", Message: "O nome pode ter no máximo 120 caracteres."}
	case !utf8.ValidString(in.Name) || strings.IndexFunc(in.Name, unicode.IsControl) >= 0:
		return in, &domain.ValidationError{Field: "name", Message: "O nome não pode ter caracteres de controle."}
	}

	if !stationCode.MatchString(in.ExternalCode) {
		return in, &domain.ValidationError{
			Field:   "external_code",
			Message: "O código deve ter de 1 a 50 caracteres: letras sem acento, números, ponto, hífen ou sublinhado.",
		}
	}

	if math.IsNaN(in.Lat) || in.Lat < minLat || in.Lat > maxLat {
		return in, &domain.ValidationError{
			Field:   "lat",
			Message: "A latitude deve estar entre -25,5 e -19,5, a faixa do Estado de São Paulo.",
		}
	}
	if math.IsNaN(in.Lon) || in.Lon < minLon || in.Lon > maxLon {
		return in, &domain.ValidationError{
			Field:   "lon",
			Message: "A longitude deve estar entre -53,5 e -44, a faixa do Estado de São Paulo.",
		}
	}

	if a := in.AltitudeM; a != nil && (*a != *a || *a < minAltitudeM || *a > maxAltitudeM) { // *a != *a only for NaN
		return in, &domain.ValidationError{
			Field:   "altitude_m",
			Message: "A altitude deve estar entre -100 e 3000 metros.",
		}
	}
	return in, nil
}
