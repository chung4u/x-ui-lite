package service

import (
	"regexp"
	"testing"
)

func TestGeneratedInboundUUIDIsIndependentVersion4(t *testing.T) {
	format := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for n := 0; n < 100; n++ {
		value, err := GenerateInboundUserUUID()
		if err != nil || !format.MatchString(value.ClientID) || seen[value.ClientID] {
			t.Fatalf("invalid or repeated credential: %v", err)
		}
		seen[value.ClientID] = true
	}
}
