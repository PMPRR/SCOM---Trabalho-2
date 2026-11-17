package domain

import "errors"

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
)

// ValidationError reports an input that breaks a business rule. Message is
// written for the end user.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
