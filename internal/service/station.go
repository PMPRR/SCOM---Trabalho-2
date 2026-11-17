// Package service holds the business rules. It depends on the data layer
// only through interfaces, so the rules can be tested without a database.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 500
)

// StationRepository is what the service needs from the data layer.
type StationRepository interface {
	List(ctx context.Context, filter domain.StationFilter, limit, offset int) ([]domain.Station, int, error)
	Get(ctx context.Context, id int64) (domain.Station, error)
	Create(ctx context.Context, ownerID int64, in domain.StationInput) (int64, error)
	Update(ctx context.Context, id int64, in domain.StationInput) error
	SetActive(ctx context.Context, id int64, active bool) error
	Delete(ctx context.Context, id int64) error
}

// Page describes which slice of a listing was returned.
type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// A reading this old no longer describes the weather now, whatever its source.
const staleAfter = 3 * time.Hour

// modelInstitutes report the output of a numerical model, not the reading of
// an instrument on site.
var modelInstitutes = map[string]bool{"openmeteo": true}

type StationService struct {
	repo StationRepository
	log  *slog.Logger
	now  func() time.Time
}

func NewStationService(repo StationRepository, log *slog.Logger) *StationService {
	return &StationService{repo: repo, log: log, now: time.Now}
}

// confidenceOf ranks the latest reading of a station. From most to least
// trusted: an official station of an institute (no owner), a private station
// registered by an operator, and last a stale reading or a model point, which
// fills the map where there is no station but must not weigh as much as a
// measurement.
func confidenceOf(st domain.Station, now time.Time) domain.Confidence {
	switch {
	case st.Latest == nil:
		return ""
	case now.Sub(st.Latest.ObservedAt) >= staleAfter:
		return domain.ConfidenceLow
	case modelInstitutes[st.InstituteCode]:
		return domain.ConfidenceLow
	case st.OwnerID == nil:
		return domain.ConfidenceHigh
	default:
		return domain.ConfidenceMedium
	}
}

// List returns one page of the stations open to the public: those turned
// on. Limit must be between 1 and MaxPageLimit and offset must not be
// negative.
func (s *StationService) List(ctx context.Context, limit, offset int) ([]domain.Station, Page, error) {
	return s.list(ctx, domain.StationFilter{ActiveOnly: true}, limit, offset)
}

func (s *StationService) list(ctx context.Context, filter domain.StationFilter, limit, offset int) ([]domain.Station, Page, error) {
	if err := checkPage(limit, offset); err != nil {
		return nil, Page{}, err
	}

	stations, total, err := s.repo.List(ctx, filter, limit, offset)
	if err != nil {
		return nil, Page{}, err
	}
	now := s.now()
	for i := range stations {
		stations[i].Confidence = confidenceOf(stations[i], now)
	}
	return stations, Page{Limit: limit, Offset: offset, Total: total}, nil
}

func checkPage(limit, offset int) error {
	if limit < 1 || limit > MaxPageLimit {
		return &domain.ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("O limite deve estar entre 1 e %d.", MaxPageLimit),
		}
	}
	if offset < 0 {
		return &domain.ValidationError{
			Field:   "offset",
			Message: "O deslocamento não pode ser negativo.",
		}
	}
	return nil
}

// Get returns one station open to the public, or domain.ErrNotFound. A
// station turned off is not found here; its manager still reaches it with
// GetManaged.
func (s *StationService) Get(ctx context.Context, id int64) (domain.Station, error) {
	station, err := s.get(ctx, id)
	if err != nil {
		return domain.Station{}, err
	}
	if !station.Active {
		return domain.Station{}, domain.ErrNotFound
	}
	return station, nil
}

func (s *StationService) get(ctx context.Context, id int64) (domain.Station, error) {
	if id < 1 {
		return domain.Station{}, &domain.ValidationError{
			Field:   "id",
			Message: "O id da estação deve ser um inteiro positivo.",
		}
	}
	station, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Station{}, err
	}
	station.Confidence = confidenceOf(station, s.now())
	return station, nil
}
