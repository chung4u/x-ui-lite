package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

func unifiedRealityInbound(t *testing.T) *model.Inbound {
	t.Helper()
	key, err := GenerateRealityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := json.Marshal(map[string]interface{}{
		"network": "tcp", "security": "reality",
		"realitySettings": map[string]interface{}{"privateKey": key.PrivateKey, "dest": "example.com:443", "serverNames": []string{"example.com"}, "shortIds": []string{"0123456789abcdef"}},
	})
	in := &model.Inbound{UserId: 1, Protocol: model.VLESS, Enable: true, Port: 19443, Tag: "reality-unified", Settings: `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","email":"first","flow":"xtls-rprx-vision","level":7,"extra":{"keep":true}}],"decryption":"none","fallbacks":[]}`, StreamSettings: string(stream)}
	if err := (&InboundService{}).AddInbound(in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestUnifiedUsersRealityCreateEditDeleteAndIsolation(t *testing.T) {
	policyTestDB(t)
	s, db := InboundService{}, database.GetDB()
	xhttp := policyTestInbound(t)
	reality := unifiedRealityInbound(t)
	beforeStream, beforeSettings := reality.StreamSettings, reality.Settings
	id := "11111111-1111-1111-1111-111111111111" // Deliberately shared across entries.
	if err := db.Create(&model.InboundSubscription{InboundId: reality.Id, ClientId: id, Token: "unified-stable-token"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.AddTraffic([]*xray.Traffic{{IsUser: true, UserTag: reality.Tag, ClientID: id, Up: 11, Down: 23}}); err != nil {
		t.Fatal(err)
	}
	change := ManagedUserChange{ClientId: id, Email: "renamed", Enabled: userEditBool(true), Total: userEditInt64(34), ExpiryTime: userEditInt64(0), ExpectedEmail: "first", ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(0), ExpectedExpiryTime: userEditInt64(0)}
	if err := s.SaveManagedUser(1, reality.Id, &change); err != nil {
		t.Fatal(err)
	}
	if err := db.First(reality, reality.Id).Error; err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Clients []map[string]interface{} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(reality.Settings), &saved); err != nil {
		t.Fatal(err)
	}
	client := saved.Clients[0]
	if client["email"] != "renamed" || client["flow"] != supportedVLESSFlow || client["level"] != float64(7) || client["extra"].(map[string]interface{})["keep"] != true || reality.StreamSettings != beforeStream {
		t.Fatal("edit changed transport or client fields")
	}
	if err := db.First(xhttp, xhttp.Id).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(xhttp.Settings, "renamed") {
		t.Fatal("same UUID in another entry was edited")
	}
	view, err := s.GetUserPolicies(reality.Id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Users[0].Up != 11 || view.Users[0].Down != 23 || view.Users[0].EffectiveEnabled || view.Users[0].Reason != "额度已用尽" {
		t.Fatalf("wrong independent accounting: %#v", view.Users[0])
	}
	if err := s.UpdateUserPolicy(reality.Id, id, true, 999); err == nil {
		t.Fatal("legacy endpoint attached a shared group to an individual account")
	}
	if err := s.UpdateUserPolicy(reality.Id, id, false, 0); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetUserPolicies(reality.Id)
	if err != nil || view.Users[0].Total != 34 || view.Users[0].Enabled {
		t.Fatal("legacy toggle overwrote individual quota", err)
	}
	if err := s.UpdateUserPolicy(reality.Id, id, true, 0); err != nil {
		t.Fatal(err)
	}
	xview, err := s.GetUserPolicies(xhttp.Id)
	if err != nil || !xview.Users[0].EffectiveEnabled || xview.Users[0].Up != 0 {
		t.Fatal("quota leaked across entries", err)
	}
	if err := s.SaveManagedUser(1, reality.Id, &change); err == nil {
		t.Fatal("stale edit accepted")
	}
	if err := s.SaveManagedUser(2, reality.Id, &change); err == nil {
		t.Fatal("other owner accepted")
	}
	create := ManagedUserChange{ClientId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Email: "new", Enabled: userEditBool(true), Total: userEditInt64(0), ExpiryTime: userEditInt64(0), Create: true}
	if err := s.SaveManagedUser(1, reality.Id, &create); err != nil {
		t.Fatal(err)
	}
	if err := db.First(reality, reality.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(reality.Settings), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Clients) != 2 || saved.Clients[1]["flow"] != supportedVLESSFlow {
		t.Fatal("REALITY user did not receive Vision")
	}
	profile, err := s.GetVLESSUserProfile(reality.Id, 1, create.ClientId, "example.com", "new")
	if err != nil || profile.Connection.Flow != supportedVLESSFlow || profile.Connection.RealityPublicKey == "" {
		t.Fatal("REALITY profile lost its parameters", err)
	}
	create.Create, create.Delete = false, true
	create.ExpectedEmail, create.ExpectedEnabled = "new", userEditBool(true)
	create.ExpectedTotal, create.ExpectedExpiryTime = userEditInt64(0), userEditInt64(0)
	if err := s.SaveManagedUser(1, reality.Id, &create); err != nil {
		t.Fatal(err)
	}
	create.Create, create.Delete = true, false
	if err := s.SaveManagedUser(1, reality.Id, &create); err == nil {
		t.Fatal("deleted credential reused")
	}
	var token model.InboundSubscription
	if err := db.Where("inbound_id = ? AND client_id = ?", reality.Id, id).First(&token).Error; err != nil || token.Token != "unified-stable-token" {
		t.Fatal("subscription changed", err)
	}
	if beforeSettings == reality.Settings {
		t.Fatal("fixture did not exercise edits")
	}
}

func TestUnifiedUsersPlainAndXHTTPDefaults(t *testing.T) {
	for _, network := range []string{"tcp", "ws", "xhttp"} {
		t.Run(network, func(t *testing.T) {
			policyTestDB(t)
			in := &model.Inbound{UserId: 1, Protocol: model.VLESS, Enable: true, Port: 19444, Tag: "plain", Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"` + network + `","security":"none","xhttpSettings":{"path":"/test","mode":"auto"}}`}
			s := InboundService{}
			if err := s.AddInbound(in); err != nil {
				t.Fatal(err)
			}
			change := ManagedUserChange{ClientId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Email: "normal", Enabled: userEditBool(true), Total: userEditInt64(0), ExpiryTime: userEditInt64(0), Create: true}
			if err := s.SaveManagedUser(1, in.Id, &change); err != nil {
				t.Fatal(err)
			}
			database.GetDB().First(in, in.Id)
			var settings vlessInboundSettings
			if err := json.Unmarshal([]byte(in.Settings), &settings); err != nil {
				t.Fatal(err)
			}
			if len(settings.Clients) != 1 || settings.Clients[0].Flow != "" {
				t.Fatal("ordinary transport received Vision")
			}
			change.ClientId = "invalid"
			if err := s.SaveManagedUser(1, in.Id, &change); err == nil {
				t.Fatal("invalid UUID accepted")
			}
			change.ClientId, change.Email = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "invalid\nname"
			if err := s.SaveManagedUser(1, in.Id, &change); err == nil {
				t.Fatal("invalid name accepted")
			}
		})
	}
}

func TestUnifiedUsersExistingPlainPoliciesAndLegacyGroups(t *testing.T) {
	policyTestDB(t)
	in := unifiedRealityInbound(t)
	s, db := InboundService{}, database.GetDB()
	id := "11111111-1111-1111-1111-111111111111"
	if err := db.Where("inbound_id = ?", in.Id).Delete(&model.InboundUserPolicy{}).Error; err != nil {
		t.Fatal(err)
	}
	change := ManagedUserChange{ClientId: id, Email: "first", Enabled: userEditBool(true), Total: userEditInt64(0), ExpiryTime: userEditInt64(0), ExpectedEmail: "first", ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(0), ExpectedExpiryTime: userEditInt64(0)}
	if err := s.SaveManagedUser(1, in.Id, &change); err != nil {
		t.Fatal("existing ordinary user without policy cannot be edited", err)
	}
	group := model.InboundQuotaGroup{InboundId: in.Id, Name: "legacy", Enabled: true, Total: 100, Up: 100}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ?", in.Id).Updates(map[string]interface{}{"quota_initialized": false, "group_id": group.Id}).Error; err != nil {
		t.Fatal(err)
	}
	change.ExpectedGroupId, change.ExpectedTotal, change.Total = group.Id, userEditInt64(100), userEditInt64(100)
	change.Email = "legacy renamed"
	if err := s.SaveManagedUser(1, in.Id, &change); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetUserPolicies(in.Id)
	if err != nil || view.Users[0].GroupId != group.Id || view.Users[0].EffectiveEnabled {
		t.Fatal("legacy shared enforcement widened", err)
	}
	change.ExpectedEmail, change.Total = "legacy renamed", userEditInt64(200)
	if err := s.SaveManagedUser(1, in.Id, &change); err == nil {
		t.Fatal("shared quota silently converted to personal quota")
	}
	db.First(in, in.Id)
	effective, err := s.effectiveInbound(in, time.Now())
	if err != nil || strings.Contains(effective.Settings, id) {
		t.Fatal("legacy exhausted user remained in runtime", err)
	}
}

func TestUnifiedUsersRejectHistoricalCredentialsWithoutPolicies(t *testing.T) {
	policyTestDB(t)
	in := unifiedRealityInbound(t)
	db := database.GetDB()
	s := InboundService{}
	ids := []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	if err := db.Create(&model.InboundUserTraffic{InboundId: in.Id, ClientId: ids[0], Up: 123}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundSubscription{InboundId: in.Id, ClientId: ids[1], Token: "historical-only-token"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		change := ManagedUserChange{ClientId: strings.ToUpper(id), Email: "reused", Enabled: userEditBool(true), Total: userEditInt64(0), ExpiryTime: userEditInt64(0), Create: true}
		if err := s.SaveManagedUser(1, in.Id, &change); err == nil {
			t.Fatal("reactivated a historical credential without a policy row")
		}
	}
}
