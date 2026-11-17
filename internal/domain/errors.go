package domain

import "errors"

// ErrNotFound is returned when the requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ValidationError reports an input that breaks a business rule. Message is
// written for the end user.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
