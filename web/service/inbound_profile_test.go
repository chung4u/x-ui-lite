package service

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"x-ui/database"
	"x-ui/database/model"
)

func TestGetVLESSUserProfileBuildsRealityClashConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	privateBytes := make([]byte, 32)
	for index := range privateBytes {
		privateBytes[index] = byte(index + 1)
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		t.Fatalf("NewPrivateKey() error = %v", err)
	}
	settings, _ := json.Marshal(vlessInboundSettings{Clients: []vlessInboundClient{{
		ID: "11111111-1111-1111-1111-111111111111", Email: "phone", Flow: "xtls-rprx-vision",
	}}})
	stream := map[string]interface{}{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]interface{}{
			"serverNames": []string{"www.example.com"},
			"privateKey":  base64.RawURLEncoding.EncodeToString(privateBytes),
			"shortIds":    []string{"abcd"},
		},
	}
	streamSettings, _ := json.Marshal(stream)
	inbound := &model.Inbound{
		UserId: 1, Port: 443, Tag: "inbound-443", Remark: "主节点", Protocol: model.VLESS,
		Settings: string(settings), StreamSettings: string(streamSettings),
	}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	profile, err := (&InboundService{}).GetVLESSUserProfile(inbound.Id, 1, "11111111-1111-1111-1111-111111111111", "node.example.com", "phone")
	if err != nil {
		t.Fatalf("GetVLESSUserProfile() error = %v", err)
	}
	publicKey := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	for _, want := range []string{
		"vless://11111111-1111-1111-1111-111111111111@node.example.com:443",
		"security=reality",
		"pbk=" + publicKey,
		"reality-opts:",
		"public-key: " + publicKey,
		"short-id: abcd",
		"servername: www.example.com",
	} {
		if !strings.Contains(profile.Link+"\n"+profile.ClashVergeConfig, want) {
			t.Fatalf("profile missing %q:\n%s\n%s", want, profile.Link, profile.ClashVergeConfig)
		}
	}
	if profile.FileName != "主节点-phone.yaml" {
		t.Fatalf("FileName = %q", profile.FileName)
	}
}

func TestGetVLESSUserProfileRejectsOtherUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	inbound := &model.Inbound{UserId: 7, Port: 2443, Tag: "inbound-2443", Protocol: model.VLESS, Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if _, err := (&InboundService{}).GetVLESSUserProfile(inbound.Id, 8, "anything", "example.com", ""); err == nil {
		t.Fatal("GetVLESSUserProfile() should reject another user's inbound")
	}
}
