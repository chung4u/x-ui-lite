package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

func policyTestDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "policy.db")); err != nil {
		t.Fatal(err)
	}
}
func policyTestInbound(t *testing.T) *model.Inbound {
	t.Helper()
	inbound := &model.Inbound{UserId: 1, Port: 18081, Listen: "172.18.0.1", Protocol: model.VLESS, Enable: true, Tag: "xhttp-main", ExternalHost: "us.example.com", ExternalPort: 443, ExternalTLS: true, Settings: `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","flow":""},{"id":"22222222-2222-2222-2222-222222222222","flow":""}],"decryption":"none"}`, StreamSettings: `{"network":"xhttp","security":"none","xhttpSettings":{"path":"/connect/test-path","mode":"auto"}}`}
	if err := (&InboundService{}).AddInbound(inbound); err != nil {
		t.Fatal(err)
	}
	return inbound
}
func TestQuotaTrafficUsesUserDeltaOnceAndSkipsArchive(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	db := database.GetDB()
	service := InboundService{}
	group := model.InboundQuotaGroup{InboundId: inbound.Id, Name: "shared", Enabled: true, Total: 150, Up: 60, Down: 60}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	// Retain regression coverage for historical rows before one-time migration.
	if err := db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ? AND client_id = ?", inbound.Id, "11111111-1111-1111-1111-111111111111").Updates(map[string]interface{}{"quota_initialized": false, "group_id": group.Id}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.AddTraffic([]*xray.Traffic{{IsInbound: true, Tag: inbound.Tag, Up: 50, Down: 70}, {IsUser: true, UserTag: inbound.Tag, ClientID: "11111111-1111-1111-1111-111111111111", Up: 10, Down: 25}, {IsUser: true, UserTag: inbound.Tag, ClientID: "22222222-2222-2222-2222-222222222222", Up: 5, Down: 9}}); err != nil {
		t.Fatal(err)
	}
	db.First(&group, group.Id)
	if group.Up != 70 || group.Down != 85 || !group.Exhausted {
		t.Fatalf("group counted wrong or not exhausted: %#v", group)
	}
	db.First(inbound, inbound.Id)
	if inbound.Up != 50 || inbound.Down != 70 {
		t.Fatalf("inbound counter wrong: %#v", inbound)
	}
	view, err := service.GetUserPolicies(inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Users[0].EffectiveEnabled || !view.Users[1].EffectiveEnabled || view.Users[0].Up != 10 {
		t.Fatalf("shared quota affected wrong users: %#v", view.Users)
	}
	if err := db.Model(inbound).Update("archived_at", time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.AddTraffic([]*xray.Traffic{{IsInbound: true, Tag: inbound.Tag, Up: 100, Down: 200}, {IsUser: true, UserTag: inbound.Tag, ClientID: "11111111-1111-1111-1111-111111111111", Up: 100, Down: 200}}); err != nil {
		t.Fatal(err)
	}
	db.First(inbound, inbound.Id)
	db.First(&group, group.Id)
	if inbound.Up != 50 || group.Up != 70 {
		t.Fatal("archived statistics were changed")
	}
}

func TestUserPoliciesFilterRuntimeAndScopeGroups(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	service := InboundService{}
	db := database.GetDB()
	other := model.InboundQuotaGroup{InboundId: 999, Name: "foreign", Enabled: true}
	db.Create(&other)
	if err := service.UpdateUserPolicy(inbound.Id, "11111111-1111-1111-1111-111111111111", true, other.Id); err == nil {
		t.Fatal("cross-inbound group accepted")
	}
	group := model.InboundQuotaGroup{InboundId: inbound.Id, Name: "expired", Enabled: true, ExpiryTime: time.Now().Add(-time.Second).UnixMilli()}
	db.Create(&group)
	if err := db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ? AND client_id = ?", inbound.Id, "11111111-1111-1111-1111-111111111111").Updates(map[string]interface{}{"quota_initialized": false, "group_id": group.Id}).Error; err != nil {
		t.Fatal(err)
	}
	effective, err := service.effectiveInbound(inbound, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var settings vlessInboundSettings
	if err = json.Unmarshal([]byte(effective.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Clients) != 1 || settings.Clients[0].ID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("wrong runtime clients: %#v", settings)
	}
	if err := ValidateInboundUserAccess(inbound, "11111111-1111-1111-1111-111111111111"); err == nil {
		t.Fatal("expired user profile allowed")
	}
	if err := ValidateInboundUserAccess(inbound, "22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateUserPolicy(inbound.Id, "22222222-2222-2222-2222-222222222222", false, 0); err != nil {
		t.Fatal(err)
	}
	effective, err = service.effectiveInbound(inbound, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(effective.Settings), &settings)
	if len(settings.Clients) != 0 {
		t.Fatal("disabled user remains in runtime")
	}
	if inbound.Settings == effective.Settings {
		t.Fatal("runtime filtering overwrote saved clients")
	}
}

func TestManagedSavePreservesTrafficAndEndpoint(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	service := InboundService{}
	db := database.GetDB()
	stale := *inbound
	db.Model(inbound).Updates(map[string]interface{}{"up": 123, "down": 456})
	stale.Remark = "renamed"
	if err := service.UpdateInbound(&stale); err != nil {
		t.Fatal(err)
	}
	saved, _ := service.GetInbound(inbound.Id)
	if saved.Up != 123 || saved.Down != 456 || saved.Tag != "xhttp-main" {
		t.Fatalf("stale form overwrote counters/tag: %#v", saved)
	}
	moved := *saved
	moved.ExternalHost = "changed.example.com"
	if err := service.UpdateInbound(&moved); err == nil {
		t.Fatal("managed endpoint change accepted")
	}
	moved = *saved
	moved.StreamSettings = `{"network":"xhttp","security":"none","xhttpSettings":{"path":"/different"}}`
	if err := service.UpdateInbound(&moved); err == nil {
		t.Fatal("managed path change accepted")
	}
	saved.ResetTraffic = true
	if err := service.UpdateInbound(saved); err != nil {
		t.Fatal(err)
	}
	saved, _ = service.GetInbound(inbound.Id)
	if saved.Up != 0 || saved.Down != 0 {
		t.Fatal("explicit reset did not clear counters")
	}
}

func TestQuotaCycleResetPreservesManualDisableAndArchivedHistory(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	service := InboundService{}
	db := database.GetDB()
	setting := SettingService{}
	setting.setInt("trafficResetDay", 1)
	db.Model(inbound).Updates(map[string]interface{}{"traffic_reset_period": "2026-10-01", "traffic_reset_day": 1, "up": 100})
	groups := []model.InboundQuotaGroup{{InboundId: inbound.Id, Name: "exhausted", Enabled: true, Exhausted: true, Up: 10, Total: 10, ResetDay: 1, Period: "2026-10-01"}, {InboundId: inbound.Id, Name: "manual", Enabled: false, Up: 10, ResetDay: 1, Period: "2026-10-01"}, {InboundId: inbound.Id, Name: "expired", Enabled: true, Up: 10, ExpiryTime: 1, ResetDay: 1, Period: "2026-10-01"}}
	db.Create(&groups)
	for index := range groups {
		db.Create(&model.InboundUserPolicy{InboundId: inbound.Id, ClientId: groups[index].Name, GroupId: groups[index].Id})
	}
	archived := model.Inbound{UserId: 1, Port: 5000, Tag: "inbound-5000", Enable: false, ArchivedAt: 1, Up: 500, TrafficExhausted: true, TrafficResetDay: 1, TrafficResetPeriod: "2026-10-01"}
	db.Create(&archived)
	restart, err := service.SyncMonthlyTrafficPeriod(time.Date(2026, time.November, 2, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !restart {
		t.Fatal("cycle reset did not request config refresh")
	}
	for index := range groups {
		db.First(&groups[index], groups[index].Id)
		if groups[index].Up != 0 || groups[index].Period != "2026-11-01" {
			t.Fatal("group did not advance cycle")
		}
	}
	if !groups[0].Enabled || groups[0].Exhausted || groups[1].Enabled || groupAccessReason(&groups[2], time.Now().UnixMilli()) == "" {
		t.Fatal("group enable/expiry semantics changed")
	}
	db.First(&archived, archived.Id)
	if archived.Up != 500 || archived.Enable || archived.TrafficResetPeriod != "2026-10-01" {
		t.Fatal("archived history was reset or re-enabled")
	}
	active, _ := service.GetAllInbounds()
	history, _ := service.GetArchivedInbounds(1)
	if len(active) != 1 || len(history) != 1 {
		t.Fatal("archive listing mixed with active rows")
	}
}

func TestXHTTPRejectsVisionAndPublicBackend(t *testing.T) {
	policyTestDB(t)
	base := policyTestInbound(t)
	bad := *base
	bad.Settings = `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}]}`
	if err := validateInboundXHTTP(&bad); err == nil {
		t.Fatal("XHTTP Vision accepted")
	}
	bad = *base
	bad.Listen = "0.0.0.0"
	if err := validateInboundXHTTP(&bad); err == nil {
		t.Fatal("managed wildcard listener accepted")
	}
	bad = *base
	bad.StreamSettings = `{"network":"xhttp","security":"none","xhttpSettings":{"path":"/../../other"}}`
	if err := validateInboundXHTTP(&bad); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestXHTTPClientFieldsCannotOverrideRuntimePolicy(t *testing.T) {
	policyTestDB(t)
	base := policyTestInbound(t)
	for _, fragment := range []string{
		`"level":1`, `"email":null`, `"email":123`, `"flow":null`,
		`"email":"bad\nname"`, `"email":"` + strings.Repeat("名", 129) + `"`,
	} {
		bad := *base
		bad.Settings = `{"clients":[{"id":"11111111-1111-1111-1111-111111111111",` + fragment + `}],"decryption":"none"}`
		if err := validateInboundXHTTP(&bad); err == nil {
			t.Fatalf("unsafe client field accepted: %s", fragment)
		}
	}
	valid := *base
	valid.Settings = `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","email":"` + strings.Repeat("名", 128) + `","flow":""}],"decryption":"none"}`
	if err := validateInboundXHTTP(&valid); err != nil {
		t.Fatalf("valid user name rejected: %v", err)
	}
}
