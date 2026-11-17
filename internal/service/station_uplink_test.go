package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// A station key, made from a fixed seed so the tests are repeatable, and
// its public half in the OpenSSH format the owner pastes in the panel.
var stationKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))

func sshLine(pub ed25519.PublicKey) string {
	str := func(b []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(b)))
		return append(out, b...)
	}
	blob := append(str([]byte("ssh-ed25519")), str(pub)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " estacao-de-teste"
}

func sign(key ed25519.PrivateKey, body string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(body)))
}

// uplinkService has Ana's station 10 sending every 10 minutes, and a clock
// the test moves.
func uplinkService(t *testing.T) (*StationService, *fakeRepo, *time.Time) {
	t.Helper()
	repo := managedStations()
	svc := newService(repo)
	in := validInput()
	in.ExternalCode = "sitio-ana"
	in.PublicKey = sshLine(stationKey.Public().(ed25519.PublicKey))
	in.ReportIntervalS = 600
	if _, err := svc.Update(context.Background(), ana, 10, in); err != nil {
		t.Fatalf("set key: %v", err)
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, repo, &now
}

func TestSubmitStoresASignedReading(t *testing.T) {
	svc, repo, now := uplinkService(t)
	body := `{"temperature_c": 23.5, "humidity_pct": 70, "precipitation_mm": 0}`

	result, err := svc.Submit(context.Background(), 10, []byte(body), sign(stationKey, body))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !result.Stored || !result.ObservedAt.Equal(*now) {
		t.Errorf("result = %+v; want stored, dated by the server clock", result)
	}
	if want := now.Add(9 * time.Minute); !result.NextAfter.Equal(want) {
		t.Errorf("next after %v, want %v (interval minus a tenth)", result.NextAfter, want)
	}
	stored := repo.readings[10]
	if len(stored) != 1 || *stored[0].TemperatureC != 23.5 || *stored[0].PrecipitationMM != 0 || stored[0].WindSpeedMS != nil {
		t.Errorf("stored %+v", stored)
	}
}

func TestSubmitRefusesBadSignatures(t *testing.T) {
	svc, repo, _ := uplinkService(t)
	body := `{"temperature_c": 23.5}`
	otherKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))

	cases := map[string]string{
		"missing":      "",
		"not base64":   "%%%",
		"another key":  sign(otherKey, body),
		"another body": sign(stationKey, `{"temperature_c": 33.5}`),
		"truncated":    sign(stationKey, body)[:40],
	}
	for name, signature := range cases {
		if _, err := svc.Submit(context.Background(), 10, []byte(body), signature); !errors.Is(err, domain.ErrInvalidSignature) {
			t.Errorf("%s: err = %v, want ErrInvalidSignature", name, err)
		}
	}
	if len(repo.readings[10]) != 0 {
		t.Error("an unsigned reading was stored")
	}
}

func TestSubmitKeepsToTheInterval(t *testing.T) {
	svc, repo, now := uplinkService(t)
	send := func(body string) (domain.SubmissionResult, error) {
		return svc.Submit(context.Background(), 10, []byte(body), sign(stationKey, body))
	}

	first := `{"observed_at": "2026-11-17T11:59:30Z", "temperature_c": 23.5}`
	if _, err := send(first); err != nil {
		t.Fatalf("first: %v", err)
	}

	// The same reading again (the station did not get the answer): accepted,
	// not stored twice, and not counted as a new submission.
	*now = now.Add(time.Minute)
	again, err := send(first)
	if err != nil || again.Stored {
		t.Errorf("resend: %+v, %v; want accepted and not stored", again, err)
	}

	// A new reading five minutes later is too soon.
	*now = now.Add(4 * time.Minute)
	_, err = send(`{"temperature_c": 23.9}`)
	var tooSoon *domain.TooSoonError
	if !errors.As(err, &tooSoon) || tooSoon.Wait != 4*time.Minute {
		t.Fatalf("five minutes later: err = %v, want TooSoonError waiting 4m", err)
	}

	// Nine minutes after the first, within the tolerance of a tenth.
	*now = now.Add(4 * time.Minute)
	if result, err := send(`{"temperature_c": 24.1}`); err != nil || !result.Stored {
		t.Errorf("nine minutes later: %+v, %v", result, err)
	}
	if len(repo.readings[10]) != 2 {
		t.Errorf("%d readings stored, want 2", len(repo.readings[10]))
	}
}

func TestSubmitChecksTheReading(t *testing.T) {
	cases := []struct {
		name, body, field string
	}{
		{"not JSON", `temperatura=23`, "body"},
		{"unknown field", `{"temperatura": 23}`, "body"},
		{"two objects", `{"temperature_c": 23}{"temperature_c": 24}`, "body"},
		{"no value", `{"observed_at": "2026-11-17T11:59:00Z"}`, "body"},
		{"temperature", `{"temperature_c": 75}`, "temperature_c"},
		{"humidity", `{"humidity_pct": -1}`, "humidity_pct"},
		{"rain", `{"precipitation_mm": 900}`, "precipitation_mm"},
		{"wind", `{"wind_speed_ms": -2}`, "wind_speed_ms"},
		{"pressure", `{"pressure_hpa": 500}`, "pressure_hpa"},
		{"direction", `{"wind_dir_deg": 400}`, "wind_dir_deg"},
		{"fractional direction", `{"wind_dir_deg": 12.5}`, "wind_dir_deg"},
		{"from the future", `{"observed_at": "2026-11-17T12:10:00Z", "temperature_c": 23}`, "observed_at"},
		{"two days old", `{"observed_at": "2026-11-15T12:00:00Z", "temperature_c": 23}`, "observed_at"},
	}
	for _, c := range cases {
		svc, repo, _ := uplinkService(t)
		_, err := svc.Submit(context.Background(), 10, []byte(c.body), sign(stationKey, c.body))
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || invalid.Field != c.field {
			t.Errorf("%s: err = %v, want ValidationError on %q", c.name, err, c.field)
		}
		if len(repo.readings[10]) != 0 {
			t.Errorf("%s: stored", c.name)
		}
	}
}

func TestSubmitNeedsAStationThatSends(t *testing.T) {
	svc, _, _ := uplinkService(t)
	body := `{"temperature_c": 23}`
	for _, id := range []int64{20, 30} { // Bruno's has no key; 30 is official
		if _, err := svc.Submit(context.Background(), id, []byte(body), sign(stationKey, body)); !errors.Is(err, domain.ErrUplinkDisabled) {
			t.Errorf("station %d: err = %v, want ErrUplinkDisabled", id, err)
		}
	}
	if _, err := svc.Submit(context.Background(), 99, []byte(body), sign(stationKey, body)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown station: err = %v, want ErrNotFound", err)
	}
}

func TestStationKeyAndInterval(t *testing.T) {
	svc := newService(managedStations())
	key := sshLine(stationKey.Public().(ed25519.PublicKey))
	cases := []struct {
		name     string
		key      string
		interval int32
		field    string
	}{
		{"RSA key", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC test", 600, "public_key"},
		{"private key pasted", "-----BEGIN OPENSSH PRIVATE KEY-----", 600, "public_key"},
		{"key without interval", key, 0, "report_interval_s"},
		{"interval too short", key, 30, "report_interval_s"},
		{"interval too long", key, 90000, "report_interval_s"},
	}
	for _, c := range cases {
		in := validInput()
		in.PublicKey, in.ReportIntervalS = c.key, c.interval
		_, err := svc.Create(context.Background(), ana, in)
		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || invalid.Field != c.field {
			t.Errorf("%s: err = %v, want ValidationError on %q", c.name, err, c.field)
		}
	}

	in := validInput()
	in.ExternalCode, in.ReportIntervalS = "sem-chave", 600
	st, err := svc.Create(context.Background(), ana, in)
	if err != nil || st.ReportIntervalS != nil || st.UplinkStatus != "" {
		t.Errorf("interval without key: %+v, %v; want no sending", st, err)
	}

	in = validInput()
	in.ExternalCode, in.PublicKey, in.ReportIntervalS = "com-chave", key+"\n", 300
	st, err = svc.Create(context.Background(), ana, in)
	if err != nil {
		t.Fatalf("with key: %v", err)
	}
	if st.KeyFingerprint == "" || st.PublicKey != key[:len(key)-len(" estacao-de-teste")] || st.UplinkStatus != domain.UplinkWaiting {
		t.Errorf("with key: %+v", st)
	}
}

func TestUplinkStatus(t *testing.T) {
	interval := int32(600)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *time.Time { t := now.Add(-ago); return &t }
	cases := []struct {
		name string
		st   domain.Station
		want domain.UplinkStatus
	}{
		{"no key", domain.Station{}, ""},
		{"never sent", domain.Station{PublicKey: "k", ReportIntervalS: &interval}, domain.UplinkWaiting},
		{"on time", domain.Station{PublicKey: "k", ReportIntervalS: &interval, LastReportAt: at(15 * time.Minute)}, domain.UplinkOK},
		{"two intervals", domain.Station{PublicKey: "k", ReportIntervalS: &interval, LastReportAt: at(20 * time.Minute)}, domain.UplinkOK},
		{"late", domain.Station{PublicKey: "k", ReportIntervalS: &interval, LastReportAt: at(21 * time.Minute)}, domain.UplinkLate},
	}
	for _, c := range cases {
		if got := uplinkStatusOf(c.st, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
