package service

import (
	"context"
	"fmt"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

const (
	DefaultPageLimit	= 50
	MaxPageLimit		= 500
)

type StationRepository interface {
	List(ctx context.Context, limit, offset int) ([]domain.Station, int, error)
	Get(ctx context.Context, id int64) (domain.Station, error)
}

type Page struct {
	Limit 	int `json:"limit"`
	Offset 	int `json:"offset"`
	Total 	int `json:"total"`
}

type StationService struct {
	repo StationRepository
}

func NewStationService(repo StationRepository) *StationService {
	return &StationService{repo: repo}
}

func (s *StationService) List(ctx context.Context, limit, offset int) ([]domain.Station, Page, error) {
	if limit < 1 || limit > MaxPageLimit {
		return nil, Page{}, &domain.ValidationError{
			Field: "offset",
			Message: "O descolamento não pode ser negativo",
		}
	}

	stations, total, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, Page{}, err
	}
	return stations, Page{Limit: limit, Offset: offset, Total: total}, nil
}

func (s *StationService) Get(ctx context.Context, id int64) (domain.Station, error) {
	if id < 1 {
		return domain.Station{}, &domain.ValidationError{
			Field: "id",
			Message: "O id da estação deve ser um inteiro positivo.",
		}
	}
	return s.repo.Get(ctx, id)
}



