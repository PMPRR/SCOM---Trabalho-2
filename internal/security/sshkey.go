package security

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
)

// Ed25519 keys in the OpenSSH format identify the stations that send their
// own readings. The owner creates the pair on any machine with
//
//	ssh-keygen -t ed25519 -N "" -f estacao
//
// and gives the server only the public half, the line of estacao.pub:
//
//	ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... comentario
//
// The station keeps the private half and signs every submission with it.
// Only Ed25519 is accepted: it is small enough for a microcontroller, and
// its signatures are deterministic, so a device without a good source of
// randomness cannot leak its key by signing.

// SSHKeyType is the only key type accepted.
const SSHKeyType = "ssh-ed25519"

// ErrSSHKey is returned for a line that is not an OpenSSH Ed25519 public key.
var ErrSSHKey = errors.New("not an OpenSSH ssh-ed25519 public key")

// SSHPublicKey is a parsed public key.
type SSHPublicKey struct {
	// Line is the key in canonical form, "ssh-ed25519 <base64>", without the
	// comment, which is free text and belongs to whoever made the key.
	Line string
	// Fingerprint is the SHA-256 fingerprint, written as ssh-keygen -l
	// shows it: "SHA256:" and the unpadded base64 of the hash.
	Fingerprint string
	Key         ed25519.PublicKey
}

// ParseSSHPublicKey reads one line in the authorized_keys format.
func ParseSSHPublicKey(line string) (SSHPublicKey, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != SSHKeyType {
		return SSHPublicKey{}, ErrSSHKey
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return SSHPublicKey{}, ErrSSHKey
	}

	// The blob is two SSH strings (a 4-byte big-endian length, then the
	// bytes): the key type again, and the 32 bytes of the key.
	keyType, rest, ok := sshString(blob)
	if !ok || string(keyType) != SSHKeyType {
		return SSHPublicKey{}, ErrSSHKey
	}
	key, rest, ok := sshString(rest)
	if !ok || len(key) != ed25519.PublicKeySize || len(rest) != 0 {
		return SSHPublicKey{}, ErrSSHKey
	}

	sum := sha256.Sum256(blob)
	return SSHPublicKey{
		Line:        SSHKeyType + " " + base64.StdEncoding.EncodeToString(blob),
		Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
		Key:         ed25519.PublicKey(key),
	}, nil
}

func sshString(b []byte) (value, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(len(b)-4) < uint64(n) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// VerifySignature reports whether signature is the Ed25519 signature of
// message by the key. The signature is the plain 64 bytes, the output of
// any Ed25519 library (libsodium, Monocypher, TweetNaCl, Python's
// cryptography), and not the SSHSIG envelope of ssh-keygen -Y sign.
func (k SSHPublicKey) VerifySignature(message, signature []byte) bool {
	return len(signature) == ed25519.SignatureSize && ed25519.Verify(k.Key, message, signature)
}
