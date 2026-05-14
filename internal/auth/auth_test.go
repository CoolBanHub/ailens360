package auth

import (
	"errors"
	"testing"
	"time"
)

func TestLoginAndVerify(t *testing.T) {
	s, err := New("admin", "secret", "test-secret", time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Login("admin", "secret"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := s.Login("admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}

	tok, exp, err := s.Issue("admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatalf("token expires in the past: %v", exp)
	}
	sub, err := s.Verify(tok)
	if err != nil || sub != "admin" {
		t.Fatalf("Verify(%q) = (%q, %v)", tok, sub, err)
	}

	if _, err := s.Verify("not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestEmptySecretRejected(t *testing.T) {
	if _, err := New("admin", "x", "", time.Hour); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestSharedSecretAcrossReplicas(t *testing.T) {
	const secret = "shared-secret-32-bytes-or-whatever"
	a, err := New("admin", "x", secret, time.Hour)
	if err != nil {
		t.Fatalf("New a: %v", err)
	}
	b, err := New("admin", "x", secret, time.Hour)
	if err != nil {
		t.Fatalf("New b: %v", err)
	}
	tok, _, err := a.Issue("admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	sub, err := b.Verify(tok)
	if err != nil || sub != "admin" {
		t.Fatalf("Verify under peer replica = (%q, %v); expected to succeed with shared secret", sub, err)
	}
}
