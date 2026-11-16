package service

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"x-ui/database"
	"x-ui/database/model"
)

func validRealityTestStream(t *testing.T, ids string) string {
	t.Helper()
	pair, err := GenerateRealityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return `{"network":"tcp","security":"reality","realitySettings":{"dest":"addons.mozilla.org:443","serverNames":["addons.mozilla.org"],"privateKey":"` + pair.PrivateKey + `","shortIds":` + ids + `}}`
}

func TestRealityShortIDsValidation(t *testing.T) {
	for _, tc := range []struct {
		name, ids string
		valid     bool
	}{
		{"empty identifier", `[""]`, true},
		{"multiple identifiers", `["", "aabb", "0123456789abcdef"]`, true},
		{"empty list", `[]`, false},
		{"null list", `null`, false},
		{"null entry", `[null]`, false},
		{"odd hex", `["abc"]`, false},
		{"non hex", `["zz"]`, false},
		{"overlong", `["0123456789abcdef00"]`, false},
		{"invalid later entry", `["aa","z0"]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbound := &model.Inbound{StreamSettings: validRealityTestStream(t, tc.ids)}
			before := inbound.StreamSettings
			if err := validateInboundReality(inbound); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if inbound.StreamSettings != before {
				t.Fatal("validation rewrote configuration")
			}
		})
	}
}

func TestInvalidRealityCannotChangeDatabase(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	s := &InboundService{}
	valid := &model.Inbound{Port: 19000, Protocol: model.VLESS, Settings: `{"clients":[]}`, StreamSettings: validRealityTestStream(t, `["","abcd"]`)}
	if err := s.AddInbound(valid); err != nil {
		t.Fatal(err)
	}
	bad := *valid
	bad.StreamSettings = validRealityTestStream(t, `[]`)
	if err := s.UpdateInbound(&bad); err == nil {
		t.Fatal("invalid update accepted")
	}
	saved, err := s.GetInbound(valid.Id)
	if err != nil || saved.StreamSettings != valid.StreamSettings {
		t.Fatalf("existing configuration changed: %v", err)
	}
	bad.Id = 0
	bad.Port = 19001
	if err := s.AddInbound(&bad); err == nil {
		t.Fatal("invalid add accepted")
	}
	good := *valid
	good.Id = 0
	good.Port = 19002
	if err := s.AddInbounds([]*model.Inbound{&good, &bad}); err == nil {
		t.Fatal("invalid bulk import accepted")
	}
	var count int64
	database.GetDB().Model(&model.Inbound{}).Count(&count)
	if count != 1 {
		t.Fatalf("invalid request changed row count: %d", count)
	}
}

func TestRealityHandshakeSettingsValidatedBeforeDatabaseWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{"missing key", func(r map[string]interface{}) { r["privateKey"] = "" }},
		{"bad key", func(r map[string]interface{}) { r["privateKey"] = "not-a-key" }},
		{"missing SNI", func(r map[string]interface{}) { r["serverNames"] = []string{} }},
		{"bad SNI", func(r map[string]interface{}) { r["serverNames"] = []string{"https://example.com"} }},
		{"IP SNI", func(r map[string]interface{}) { r["serverNames"] = []string{"127.0.0.1"} }},
		{"missing destination", func(r map[string]interface{}) { r["dest"] = "" }},
		{"bad port", func(r map[string]interface{}) { r["dest"] = "example.com:65536" }},
		{"destination URL", func(r map[string]interface{}) { r["dest"] = "https://example.com:443" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stream map[string]interface{}
			if err := json.Unmarshal([]byte(validRealityTestStream(t, `["0123456789abcdef"]`)), &stream); err != nil {
				t.Fatal(err)
			}
			tc.mutate(stream["realitySettings"].(map[string]interface{}))
			encoded, _ := json.Marshal(stream)
			inbound := &model.Inbound{StreamSettings: string(encoded)}
			if err := validateInboundReality(inbound); err == nil {
				t.Fatal("invalid handshake configuration accepted")
			}
		})
	}
}
