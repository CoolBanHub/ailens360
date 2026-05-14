package shortid

import (
	"crypto/rand"
	"math/big"
)

const (
	alphabet      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	alphabetLower = "0123456789abcdefghijklmnopqrstuvwxyz"
)

// New returns a cryptographically random base62 string of the given length.
func New(n int) (string, error) {
	if n <= 0 {
		n = 16
	}
	return newFromAlphabet(n, alphabet)
}

// NewLower returns a cryptographically random string of the given length using
// only lowercase letters and digits.
func NewLower(n int) (string, error) {
	if n <= 0 {
		n = 4
	}
	return newFromAlphabet(n, alphabetLower)
}

func newFromAlphabet(n int, alpha string) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(alpha)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alpha[idx.Int64()]
	}
	return string(out), nil
}

// MustNew panics on error. Use only at startup.
func MustNew(n int) string {
	s, err := New(n)
	if err != nil {
		panic(err)
	}
	return s
}
