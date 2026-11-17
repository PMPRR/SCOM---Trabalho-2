// Package security holds the cryptographic pieces of authentication:
// password hashing, session tokens and the login rate limiter.
package security

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost makes one hash take roughly a quarter of a second on
// current hardware: unnoticeable at login, expensive for whoever tries to
// guess passwords from a stolen table.
const DefaultBcryptCost = 12

// MaxPasswordBytes is the limit of bcrypt itself, which ignores everything
// after the 72nd byte. Longer passwords are refused instead of being
// silently truncated.
const MaxPasswordBytes = 72

// Bcrypt hashes passwords with bcrypt. The salt is random per password and
// travels inside the hash, together with the cost.
type Bcrypt struct {
	Cost int
}

func NewBcrypt() Bcrypt { return Bcrypt{Cost: DefaultBcryptCost} }

func (b Bcrypt) Hash(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", errors.New("password longer than bcrypt accepts")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), b.Cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Verify reports whether password matches hash. The comparison inside bcrypt
// takes the same time whether the password is right or wrong.
func (b Bcrypt) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
