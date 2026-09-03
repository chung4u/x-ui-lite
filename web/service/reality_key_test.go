package service

import (
	"regexp"
	"testing"
)

func TestGenerateRealityKeyPair(t *testing.T) {
	pair, err := GenerateRealityKeyPair()
	if err != nil {
		t.Fatalf("GenerateRealityKeyPair() error = %v", err)
	}
	if pair.PrivateKey == "" || pair.PublicKey == "" {
		t.Fatalf("pair = %#v", pair)
	}
	derived, err := GetRealityPublicKey(pair.PrivateKey)
	if err != nil {
		t.Fatalf("GetRealityPublicKey() error = %v", err)
	}
	if derived.PublicKey != pair.PublicKey {
		t.Fatalf("public key = %q, want %q", derived.PublicKey, pair.PublicKey)
	}
}

func TestGenerateRealityShortID(t *testing.T) {
	shortID, err := GenerateRealityShortID()
	if err != nil {
		t.Fatalf("GenerateRealityShortID() error = %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(shortID.ShortID) {
		t.Fatalf("ShortID = %q, want 16 lowercase hexadecimal characters", shortID.ShortID)
	}
}
