package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
	"github.com/PMPRR/SCOM---Trabalho-2/internal/security"
)

// Readings sent by the private stations themselves.
//
// The station signs the exact bytes of the JSON body with its Ed25519
// private key and sends the signature, in base64, next to it. The server
// checks it against the public key the owner registered; nothing secret is
// stored on the server, and a reading cannot be changed on the way without
// the signature failing. A captured submission sent again is harmless: the
// reading is already stored under the same time, and nothing changes.
//
// The station must keep to the interval its owner chose. A small tolerance
// (a tenth of the interval) absorbs the drift of a device's clock and the
// time a request takes; anything faster is refused.

const (
	// maxSubmissionAge is how old a reading may be when it arrives: a
	// station that was offline can still send the last reading it took.
	maxSubmissionAge = 24 * time.Hour
	// maxSubmissionAdvance is how far in the future a reading may be dated,
	// for a device whose clock runs a little ahead.
	maxSubmissionAdvance = 5 * time.Minute
)

// reading is the JSON body a station sends. Pointers tell a missing sensor
// from a zero: 0 mm of rain is a measurement.
type reading struct {
	ObservedAt      *time.Time `json:"observed_at"`
	TemperatureC    *float64   `json:"temperature_c"`
	HumidityPct     *float64   `json:"humidity_pct"`
	PrecipitationMM *float64   `json:"precipitation_mm"`
	WindSpeedMS     *float64   `json:"wind_speed_ms"`
	WindDirDeg      *float64   `json:"wind_dir_deg"`
	PressureHPa     *float64   `json:"pressure_hpa"`
}

// Submit receives a reading sent by station id. payload is the body exactly
// as it arrived, and signature the base64 of its Ed25519 signature.
func (s *StationService) Submit(ctx context.Context, id int64, payload []byte, signature string) (domain.SubmissionResult, error) {
	now := s.now().UTC()

	station, err := s.get(ctx, id)
	if err != nil {
		return domain.SubmissionResult{}, err
	}
	if station.IsOfficial() || station.PublicKey == "" || station.ReportIntervalS == nil {
		return domain.SubmissionResult{}, domain.ErrUplinkDisabled
	}

	// The signature comes before anything else: an unsigned body is not
	// even read.
	key, err := security.ParseSSHPublicKey(station.PublicKey)
	if err != nil {
		return domain.SubmissionResult{}, err // stored key is checked on the way in; this is a bug
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || !key.VerifySignature(payload, sig) {
		s.log.Warn("submission refused", "station_id", id, "reason", "invalid signature")
		return domain.SubmissionResult{}, domain.ErrInvalidSignature
	}

	obs, err := parseReading(payload, now)
	if err != nil {
		return domain.SubmissionResult{}, err
	}

	interval := time.Duration(*station.ReportIntervalS) * time.Second
	minGap := interval - interval/10
	stored, err := s.repo.SaveSubmission(ctx, id, obs, now, now.Add(-minGap))
	if err != nil {
		var tooSoon *domain.TooSoonError
		if errors.As(err, &tooSoon) {
			s.log.Warn("submission refused", "station_id", id, "reason", "before the interval", "wait", tooSoon.Wait)
		}
		return domain.SubmissionResult{}, err
	}

	result := domain.SubmissionResult{Stored: stored, ObservedAt: obs.ObservedAt, NextAfter: ceilSecond(now.Add(minGap))}
	if stored {
		s.log.Info("reading received", "station_id", id, "observed_at", obs.ObservedAt)
	} else if station.LastReportAt != nil {
		// The same reading again: the previous submission still counts.
		result.NextAfter = ceilSecond(station.LastReportAt.UTC().Add(minGap))
	}
	return result, nil
}

// ceilSecond rounds up to the whole second. A device that waits until the
// time it was given is then never a fraction of a second too early.
func ceilSecond(t time.Time) time.Time {
	if r := t.Truncate(time.Second); !r.Equal(t) {
		return r.Add(time.Second)
	}
	return t
}

// parseReading reads and checks the body. The limits are the CHECK
// constraints of the observations table. Unlike the collection from the
// institutes, which drops an impossible value and keeps the rest, here a
// bad value refuses the whole reading: the owner must know the sensor or
// the firmware is wrong.
func parseReading(payload []byte, now time.Time) (domain.Observation, error) {
	var in reading
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || decoder.More() {
		return domain.Observation{}, &domain.ValidationError{
			Field:   "body",
			Message: "O corpo não é um JSON de leitura válido. Confira os nomes e os tipos dos campos.",
		}
	}

	obs := domain.Observation{ObservedAt: now.Truncate(time.Second)}
	if in.ObservedAt != nil {
		obs.ObservedAt = in.ObservedAt.UTC()
		if obs.ObservedAt.After(now.Add(maxSubmissionAdvance)) || obs.ObservedAt.Before(now.Add(-maxSubmissionAge)) {
			return domain.Observation{}, &domain.ValidationError{
				Field:   "observed_at",
				Message: "A data da leitura deve estar entre 24 horas atrás e 5 minutos à frente do relógio do servidor.",
			}
		}
	}

	checks := []struct {
		field    string
		value    *float64
		min, max float64
		message  string
		dest     **float32
	}{
		{"temperature_c", in.TemperatureC, -90, 60, "A temperatura deve estar entre -90 e 60 °C.", &obs.TemperatureC},
		{"humidity_pct", in.HumidityPct, 0, 100, "A umidade deve estar entre 0 e 100 %.", &obs.HumidityPct},
		{"precipitation_mm", in.PrecipitationMM, 0, 500, "A chuva deve estar entre 0 e 500 mm.", &obs.PrecipitationMM},
		{"wind_speed_ms", in.WindSpeedMS, 0, 120, "A velocidade do vento deve estar entre 0 e 120 m/s.", &obs.WindSpeedMS},
		{"pressure_hpa", in.PressureHPa, 800, 1100, "A pressão deve estar entre 800 e 1100 hPa.", &obs.PressureHPa},
	}
	measured := false
	for _, c := range checks {
		if c.value == nil {
			continue
		}
		if math.IsNaN(*c.value) || *c.value < c.min || *c.value > c.max {
			return domain.Observation{}, &domain.ValidationError{Field: c.field, Message: c.message}
		}
		v := float32(*c.value)
		*c.dest = &v
		measured = true
	}
	if in.WindDirDeg != nil {
		d := *in.WindDirDeg
		if d < 0 || d > 360 || d != math.Trunc(d) {
			return domain.Observation{}, &domain.ValidationError{
				Field:   "wind_dir_deg",
				Message: "A direção do vento deve ser um número inteiro de 0 a 360 graus.",
			}
		}
		dir := int16(d)
		obs.WindDirDeg = &dir
		measured = true
	}
	if !measured {
		return domain.Observation{}, &domain.ValidationError{
			Field:   "body",
			Message: "A leitura precisa de pelo menos um valor medido.",
		}
	}
	return obs, nil
}
