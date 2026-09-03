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
			"serverNames":       []string{"www.example.com"},
			"privateKey":        base64.RawURLEncoding.EncodeToString(privateBytes),
			"shortIds":          []string{"abcd"},
			"clientFingerprint": "firefox",
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
		"client-fingerprint: firefox",
	} {
		if !strings.Contains(profile.Link+"\n"+profile.ClashVergeConfig, want) {
			t.Fatalf("profile missing %q:\n%s\n%s", want, profile.Link, profile.ClashVergeConfig)
		}
	}
	if profile.FileName != "主节点-phone.yaml" {
		t.Fatalf("FileName = %q", profile.FileName)
	}
	if got := profile.Connection; got.Name != "主节点 · phone" || got.Type != "vless" || got.Server != "node.example.com" || got.Port != 443 || got.UUID != "11111111-1111-1111-1111-111111111111" || got.Encryption != "none" || got.Network != "tcp" || !got.UDP || !got.TLS || got.ServerName != "www.example.com" || got.Flow != "xtls-rprx-vision" || got.ClientFingerprint != "firefox" || got.RealityPublicKey != publicKey {
		t.Fatalf("Connection = %#v", got)
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

func TestVLESSSubscriptionUsesStableTokenAndExpiresWithRemovedUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	const clientID = "22222222-2222-2222-2222-222222222222"
	inbound := &model.Inbound{UserId: 4, Port: 8443, Tag: "inbound-8443", Protocol: model.VLESS, Settings: `{"clients":[{"id":"` + clientID + `","email":"User 1"}]}`, StreamSettings: `{"network":"tcp","security":"none"}`}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	service := &InboundService{}
	token, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil {
		t.Fatalf("create subscription token: %v", err)
	}
	again, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil || again != token {
		t.Fatalf("subscription token should stay stable: %q, %v", again, err)
	}
	profile, err := service.GetVLESSUserSubscription(token, "node.example.com")
	if err != nil || !strings.Contains(profile.ClashVergeConfig, "server: node.example.com") || !strings.Contains(profile.Link, clientID) {
		t.Fatalf("GetVLESSUserSubscription() = %#v, %v", profile, err)
	}
	inbound.Settings = `{"clients":[]}`
	if err := database.GetDB().Save(inbound).Error; err != nil {
		t.Fatalf("remove client: %v", err)
	}
	if _, err := service.GetVLESSUserSubscription(token, "node.example.com"); err == nil {
		t.Fatal("removed user subscription should be invalid")
	}
}
