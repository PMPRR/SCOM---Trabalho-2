package domain

import (
	"errors"
	"time"
)

// ErrNotFound is returned when the requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Errors of authentication and access control. Each one maps to a single
// HTTP status in the render package.
var (
	// ErrInvalidCredentials covers both an unknown e-mail and a wrong
	// password: telling them apart would reveal which e-mails have accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUnauthenticated means the request needs a logged-in user.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden means the user is known but may not do this.
	ErrForbidden = errors.New("forbidden")
	// ErrEmailTaken means another account already uses the e-mail.
	ErrEmailTaken = errors.New("email already registered")
	// ErrTooManyRequests means the caller exceeded a rate limit.
	ErrTooManyRequests = errors.New("too many requests")
)

// Errors of station management.
var (
	// ErrOfficialStation means a change that only stations registered on
	// the site accept was asked of a station collected from an institute.
	ErrOfficialStation = errors.New("official station cannot be changed")
	// ErrStationCodeTaken means another station registered on the site
	// already uses the code.
	ErrStationCodeTaken = errors.New("station code already registered")
	// ErrKeyTaken means another station already uses the public key.
	ErrKeyTaken = errors.New("public key already registered")
)

// Errors of readings sent by the stations themselves.
var (
	// ErrInvalidSignature means the submission is not signed by the key of
	// the station: missing, malformed or wrong signature.
	ErrInvalidSignature = errors.New("invalid signature")
	// ErrUplinkDisabled means the station has no key and accepts no
	// submissions.
	ErrUplinkDisabled = errors.New("station does not send readings")
)

// TooSoonError means a station sent a reading before its interval was over.
// Wait is how long until the next submission is accepted.
type TooSoonError struct {
	Wait time.Duration
}

func (e *TooSoonError) Error() string {
	return "submission before the interval: wait " + e.Wait.String()
}

// ValidationError reports an input that breaks a business rule. Message is
// written for the end user.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
