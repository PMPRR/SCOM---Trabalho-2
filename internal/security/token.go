package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// NewToken returns 256 bits from the system's random source, as text safe
// for a cookie. It is used for session tokens and CSRF tokens.
func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken is what the database stores in place of a session token. SHA-256
// without salt is enough here, unlike for passwords: the token is already
// 256 random bits, so there is nothing to guess.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// EqualTokens compares in constant time, so the time taken does not reveal
// how many leading characters were right.
func EqualTokens(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
