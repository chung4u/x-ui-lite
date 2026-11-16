package credential

import (
	"strings"
	"testing"
)

func TestPasswordHashAndBoundedVerification(t *testing.T) {
	password := " spaces are significant "
	a, err := Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, password) || !Verify(a, password) || Verify(a, strings.TrimSpace(password)) {
		t.Fatal("password or salt handling failed")
	}
	for _, malformed := range []string{"", "admin", "$argon2id$v=19$m=999999999,t=2,p=1$AA$AA", prefix + "AA$AA", prefix + "bad"} {
		if Verify(malformed, password) {
			t.Fatal("malformed hash accepted")
		}
	}
	if Verify(a, strings.Repeat("a", 1025)) || ValidateNew("admin") == nil || ValidateNew(password) != nil {
		t.Fatal("password bounds failed")
	}
}
