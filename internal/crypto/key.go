package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

// GenerateKey returns a base64-encoded 32-byte random key. Used by `ailens360 keygen`.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
