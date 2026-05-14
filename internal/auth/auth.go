// Package auth implements console login: a single-admin credential check + JWT
// (HS256) issuance and verification.
//
// AILens360 is a self-hosted observability tool, so a single-admin model is
// sufficient. Multi-user / projects-RBAC can be layered on later.
package auth

import (
	"crypto/subtle"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrEmptySecret        = errors.New("auth: jwt secret is required")
)

// Service holds the admin credential and signing key.
type Service struct {
	username string
	password string
	secret   []byte
	ttl      time.Duration
}

// New constructs an auth service. jwtSecret MUST be non-empty — the old
// single-machine fallback (random-secret-per-restart) was removed because
// each replica would mint tokens that no other replica can verify, breaking
// the LB.
func New(username, password, jwtSecret string, ttl time.Duration) (*Service, error) {
	if jwtSecret == "" {
		return nil, ErrEmptySecret
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Service{
		username: username,
		password: password,
		secret:   []byte(jwtSecret),
		ttl:      ttl,
	}, nil
}

// Login verifies the credentials. Returns ErrInvalidCredentials on mismatch.
// We compare in constant time to avoid timing oracles, even for a 1-user setup.
func (s *Service) Login(username, password string) error {
	uOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
	pOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
	if !uOK || !pOK {
		return ErrInvalidCredentials
	}
	return nil
}

// Issue mints a JWT that expires after the configured TTL.
func (s *Service) Issue(username string) (string, time.Time, error) {
	exp := time.Now().Add(s.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": username,
		"exp": exp.Unix(),
		"iat": time.Now().Unix(),
	})
	signed, err := tok.SignedString(s.secret)
	return signed, exp, err
}

// Verify checks the token signature + expiry and returns the subject.
func (s *Service) Verify(raw string) (string, error) {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !tok.Valid {
		return "", ErrInvalidToken
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", ErrInvalidToken
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", ErrInvalidToken
	}
	return sub, nil
}

// TTL returns the configured token lifetime so the login response can advertise it.
func (s *Service) TTL() time.Duration { return s.ttl }
