package singbox

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// REALITY keys are raw X25519 keys, base64url-encoded without padding.
// Clients cannot connect without the public half, so any code path that only
// has the server private key must be able to derive it again.

func encodeRealityKey(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeRealityKey accepts the raw-url form used by sing-box as well as
// padded/std variants people paste in by hand.
func decodeRealityKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "=")
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// GenerateRealityKeypair returns (privateKey, publicKey) exactly in the format
// `sing-box generate reality-keypair` prints. Doing it in-process removes the
// dependency on the sing-box binary being present on PATH.
func GenerateRealityKeypair() (string, string, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return encodeRealityKey(key.Bytes()), encodeRealityKey(key.PublicKey().Bytes()), nil
}

// DeriveRealityPublicKey recomputes the client-side public key from a server
// private key. Returns "" when the input is not a valid X25519 key.
func DeriveRealityPublicKey(privateKey string) string {
	raw, err := decodeRealityKey(privateKey)
	if err != nil || len(raw) != 32 {
		return ""
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return ""
	}
	return encodeRealityKey(key.PublicKey().Bytes())
}
