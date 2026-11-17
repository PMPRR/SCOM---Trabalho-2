// Package ingest reads observations from external weather services and
// converts them to domain.Reading. Each source lives in its own file and
// knows only its own API; validation and storage happen in other layers.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	userAgent    = "ClimaSP/0.1 (projeto academico; github.com/PMPRR/SCOM---Trabalho-2)"
	maxBodyBytes = 32 << 20 // a response larger than this is not a weather report
)

// statusError is an answer with an HTTP status other than 200.
type statusError struct {
	Code int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected status %d", e.Code)
}

// getJSON performs a GET and decodes the JSON body into out.
func getJSON(ctx context.Context, client *http.Client, rawURL string, out any) error {
	return requestJSON(ctx, client, http.MethodGet, rawURL, nil, nil, out)
}

// requestJSON performs a request and decodes the JSON body into out. A
// non-nil body is sent as JSON. An empty answer or 204 leaves out untouched:
// several services answer that way when they have no data.
//
// Errors never contain the request URL, headers or body, because that is
// where services carry tokens and passwords, and errors end up in the logs.
func requestJSON(ctx context.Context, client *http.Client, method, rawURL string, headers map[string]string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errors.New("build request: body cannot be encoded")
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, payload)
	if err != nil {
		return errors.New("build request: invalid URL")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", withoutURL(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return &statusError{Code: resp.StatusCode}
	}

	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// withoutURL unwraps the *url.Error that net/http returns, whose message
// starts with the full request URL.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// number decodes a JSON number, a numeric string ("24.3") or null. Public
// weather APIs mix the three for the same field, and some write a word where
// a number should be (wind direction "VRB"); anything that is not a number
// counts as missing rather than as an error.
type number struct {
	value float64
	ok    bool
}

func (n *number) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	n.value, n.ok = value, true
	return nil
}

// float32Ptr returns nil for a missing value, the form domain.Observation
// uses for a sensor the station does not have.
func (n number) float32Ptr() *float32 {
	if !n.ok {
		return nil
	}
	v := float32(n.value)
	return &v
}

func (n number) int16Ptr() *int16 {
	if !n.ok {
		return nil
	}
	v := int16(n.value)
	return &v
}
