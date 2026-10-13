package mesh

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestNewKeyPair(t *testing.T) {
	kp1, err := NewKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	privBytes, err := base64.StdEncoding.DecodeString(kp1.Private)
	if err != nil || len(privBytes) != 32 {
		t.Fatalf("invalid private key: len=%d, err=%v", len(privBytes), err)
	}

	pubBytes, err := base64.StdEncoding.DecodeString(kp1.Public)
	if err != nil || len(pubBytes) != 32 {
		t.Fatalf("invalid public key: len=%d, err=%v", len(pubBytes), err)
	}

	// Verify uniqueness
	kp2, err := NewKeyPair()
	if err != nil {
		t.Fatalf("failed to generate second key pair: %v", err)
	}
	if kp1.Private == kp2.Private || kp1.Public == kp2.Public {
		t.Errorf("expected distinct key pairs, got identical")
	}
}

func TestB64ToHex(t *testing.T) {
	raw := []byte("hello-world-mesh-key-test-bytes!") // 32 bytes
	b64 := base64.StdEncoding.EncodeToString(raw)
	expectedHex := hex.EncodeToString(raw)

	gotHex, err := b64ToHex(b64)
	if err != nil {
		t.Fatalf("b64ToHex failed: %v", err)
	}
	if gotHex != expectedHex {
		t.Errorf("got %q, want %q", gotHex, expectedHex)
	}

	// Invalid base64
	_, err = b64ToHex("!!!invalid-base64&&&")
	if err == nil {
		t.Error("expected error on invalid base64 string")
	}
}

func TestUp_InvalidMeshIP(t *testing.T) {
	_, err := Up(Config{
		PrivateKey: "AAAA",
		MeshIP:     "not-an-ip-address",
	})
	if err == nil {
		t.Error("expected error for bad mesh IP")
	}
}
