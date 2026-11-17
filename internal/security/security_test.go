package security

import (
	"strings"
	"testing"
	"time"
)

func TestBcryptHashVerifies(t *testing.T) {
	hasher := Bcrypt{Cost: 4} // lowest cost: this test is about behaviour, not strength

	hash, err := hasher.Hash("correta-bateria-grampo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(hash, "correta-bateria-grampo") {
		t.Error("the hash contains the password")
	}
	if !hasher.Verify(hash, "correta-bateria-grampo") {
		t.Error("the right password was refused")
	}
	if hasher.Verify(hash, "correta-bateria-grampO") {
		t.Error("a wrong password was accepted")
	}
}

func TestBcryptSaltsEveryHash(t *testing.T) {
	hasher := Bcrypt{Cost: 4}

	first, _ := hasher.Hash("mesma-senha")
	second, _ := hasher.Hash("mesma-senha")
	if first == second {
		t.Error("two hashes of the same password are equal: no salt")
	}
}

func TestBcryptRefusesPasswordItWouldTruncate(t *testing.T) {
	if _, err := (Bcrypt{Cost: 4}).Hash(strings.Repeat("a", MaxPasswordBytes+1)); err == nil {
		t.Error("want an error for a password longer than 72 bytes")
	}
}

func TestTokensAreUniqueAndHashedForStorage(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, _ := NewToken()

	if first == second || len(first) < 43 {
		t.Errorf("tokens = %q, %q", first, second)
	}
	if hash := HashToken(first); len(hash) != 32 || string(hash) == first {
		t.Errorf("hash has %d bytes", len(hash))
	}
	if !EqualTokens(first, first) || EqualTokens(first, second) || EqualTokens("", "") {
		t.Error("EqualTokens gave a wrong answer")
	}
}

func TestRateLimiterBlocksAfterTheLimitAndRecovers(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(3, time.Minute)
	limiter.now = func() time.Time { return now }

	for i := 1; i <= 3; i++ {
		if ok, _ := limiter.Allow("10.0.0.1"); !ok {
			t.Fatalf("attempt %d was blocked, want allowed", i)
		}
	}
	ok, wait := limiter.Allow("10.0.0.1")
	if ok || wait != time.Minute {
		t.Errorf("fourth attempt: allowed = %v, wait = %v; want blocked for 1m", ok, wait)
	}
	if ok, _ := limiter.Allow("10.0.0.2"); !ok {
		t.Error("another address was blocked by the first one")
	}

	now = now.Add(time.Minute)
	if ok, _ := limiter.Allow("10.0.0.1"); !ok {
		t.Error("still blocked after the window ended")
	}
}
