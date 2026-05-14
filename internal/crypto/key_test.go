package crypto

import (
	"encoding/base64"
	"testing"
)

func TestGenerateKeyReturnsBase64Encoded32ByteKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("decode generated key: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("generated key length = %d, want 32", len(raw))
	}
}
