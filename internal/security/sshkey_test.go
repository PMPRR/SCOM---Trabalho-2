package security

import (
	"encoding/base64"
	"testing"
)

// Keys made with ssh-keygen -t ed25519 (OpenSSH 9.6); the fingerprints are
// what ssh-keygen -lf printed for them. The signature was made with the
// private half of the first key by Python's cryptography library.
const (
	testKeyLine        = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICCok5BPxUHKiVYF/AhtFDYjBTZzG5R9SW9cneiuo26I estacao-sitio-01"
	testKeyFingerprint = "SHA256:UCrXsHEbDc+zm6KnEX2ToWVcY4qV7Z1c7dnKKyKUvHE"
	otherKeyLine       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID01IY0dQ5XpAzVyZwzlegsn0xQQnRRGOF6lLGm/konw outra"
	testMessage        = `{"temperature_c":24.3}`
	testSignature      = "3YGjr5/qPmbr/gE/XfKbazpvkVUrRBwp9KK1+aVCxyyZcbOpcWwIJdP2fLcV4au/I0788z7x2N6PFvlIISjaBA=="
)

func TestParseSSHPublicKeyMatchesSSHKeygen(t *testing.T) {
	key, err := ParseSSHPublicKey("  " + testKeyLine + "\n")
	if err != nil {
		t.Fatalf("ParseSSHPublicKey: %v", err)
	}
	if key.Fingerprint != testKeyFingerprint {
		t.Errorf("fingerprint = %s, want %s", key.Fingerprint, testKeyFingerprint)
	}
	if key.Line != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICCok5BPxUHKiVYF/AhtFDYjBTZzG5R9SW9cneiuo26I" {
		t.Errorf("canonical line keeps the comment or changed: %q", key.Line)
	}
}

func TestParseSSHPublicKeyRefusals(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"RSA key":       "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCZvoB6269HGQ1svmcHnnCCS6AqrU9VRapzQr1soc1kKZEyCgKPuEqISl8DBzGFHdjavziWLflv038ZyDN1aJ1ebZ6PlfSDOh/tmAs3ytsCm6hUlcfhXnG+/X2HnIUSpa3QS7CUuUHfLIzdn2ehPGmBhf1a9xALRL7P1Irkd0FJSwHs+2UIQMLUUjKY7LWacLIiEeQNSFcma6xMQ3Hz8jkIPGAOeYQV5NVXERNPYvs5gd3UpuoJnba5ozyafGCfbNLU1GCGPNUPV7vycDRxm0IG/cViHuAUWjoDhnZwQeyqoK5WTvWpmsYsJEZXNLT4fNCyuX9bSZOau6NknZ9FEHw9 rsa",
		"type only":     "ssh-ed25519",
		"bad base64":    "ssh-ed25519 não-é-base64",
		"type mismatch": "ssh-ed25519 AAAAB3NzaC1yc2EAAAADAQABAAABAQCZ",
		"truncated":     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICCok5BPxUHKiVYF",
		"private key":   "-----BEGIN OPENSSH PRIVATE KEY-----",
	}
	for name, line := range cases {
		if _, err := ParseSSHPublicKey(line); err == nil {
			t.Errorf("%s: accepted %q", name, line)
		}
	}
}

func TestVerifySignature(t *testing.T) {
	key, _ := ParseSSHPublicKey(testKeyLine)
	other, _ := ParseSSHPublicKey(otherKeyLine)
	sig, _ := base64.StdEncoding.DecodeString(testSignature)

	if !key.VerifySignature([]byte(testMessage), sig) {
		t.Error("a valid signature was refused")
	}
	if key.VerifySignature([]byte(`{"temperature_c":34.3}`), sig) {
		t.Error("a changed message was accepted")
	}
	if other.VerifySignature([]byte(testMessage), sig) {
		t.Error("another station's key accepted the signature")
	}
	if key.VerifySignature([]byte(testMessage), sig[:63]) {
		t.Error("a short signature was accepted")
	}
}
