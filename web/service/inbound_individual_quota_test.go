package service

import (
	"encoding/json"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

func TestIndividualQuotasEnforceOnlyTheAccountAndPreserveCounters(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	s := InboundService{}
	db := database.GetDB()
	first, second := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	for _, id := range []string{first, second} {
		change := ManagedUserChange{ClientId: id, Enabled: userEditBool(true), Total: userEditInt64(100), ExpiryTime: userEditInt64(0), ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(0), ExpectedExpiryTime: userEditInt64(0)}
		if err := s.SaveManagedUser(1, inbound.Id, &change); err != nil {
			t.Fatal(err)
		}
	}
	history := model.InboundQuotaGroup{InboundId: inbound.Id, Name: "historical", Enabled: true, Total: 100, Up: 7, Down: 8}
	db.Create(&history)
	db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ?", inbound.Id).Update("group_id", history.Id)
	if err := s.AddTraffic([]*xray.Traffic{
		{IsUser: true, UserTag: inbound.Tag, ClientID: first, Up: 45, Down: 55},
		{IsUser: true, UserTag: inbound.Tag, ClientID: second, Up: 10, Down: 20},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetUserPolicies(inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Users[0].EffectiveEnabled || !view.Users[1].EffectiveEnabled || view.Users[0].Reason != "额度已用尽" || view.Users[1].Total != 100 {
		t.Fatalf("accounts share enforcement: %#v", view.Users)
	}
	if err := ValidateInboundUserAccess(inbound, first); err == nil {
		t.Fatal("exhausted export allowed")
	}
	if err := ValidateInboundUserAccess(inbound, second); err != nil {
		t.Fatal(err)
	}
	effective, err := s.effectiveInbound(inbound, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var settings vlessInboundSettings
	json.Unmarshal([]byte(effective.Settings), &settings)
	if len(settings.Clients) != 1 || settings.Clients[0].ID != second {
		t.Fatal("runtime did not isolate exhausted account")
	}
	// Raising only the exhausted account's allowance revives it without resetting use.
	change := ManagedUserChange{ClientId: first, Enabled: userEditBool(true), Total: userEditInt64(200), ExpiryTime: userEditInt64(0), ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(100), ExpectedExpiryTime: userEditInt64(0)}
	if err := s.SaveManagedUser(1, inbound.Id, &change); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInboundUserAccess(inbound, first); err != nil {
		t.Fatal(err)
	}
	var traffic model.InboundUserTraffic
	db.Where("inbound_id = ? AND client_id = ?", inbound.Id, first).First(&traffic)
	if traffic.Up != 45 || traffic.Down != 55 {
		t.Fatal("allowance edit reset counters")
	}
	db.First(&history, history.Id)
	var savedPolicy model.InboundUserPolicy
	db.Where("inbound_id = ? AND client_id = ?", inbound.Id, first).First(&savedPolicy)
	if history.Up != 7 || history.Down != 8 || savedPolicy.GroupId != history.Id {
		t.Fatal("independent traffic or editing overwrote historical group data")
	}
	if err := s.SaveManagedUser(1, inbound.Id, &change); err == nil {
		t.Fatal("stale allowance edit accepted")
	}
	if err := s.UpdateUserPolicy(inbound.Id, first, true, 1); err == nil {
		t.Fatal("legacy endpoint restored shared quota")
	}
	if _, err := s.UpdateQuotaGroup(inbound.Id, &model.InboundQuotaGroup{Name: "forbidden", Enabled: true}); err == nil {
		t.Fatal("legacy endpoint created shared quota")
	}
}

func TestIndividualQuotaExpiryAndCycleReset(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	s := InboundService{}
	db := database.GetDB()
	id := "11111111-1111-1111-1111-111111111111"
	change := ManagedUserChange{ClientId: id, Enabled: userEditBool(true), Total: userEditInt64(100), ExpiryTime: userEditInt64(1), ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(0), ExpectedExpiryTime: userEditInt64(0)}
	if err := s.SaveManagedUser(1, inbound.Id, &change); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInboundUserAccess(inbound, id); err == nil {
		t.Fatal("expired personal account allowed")
	}
	db.Model(inbound).Updates(map[string]interface{}{"traffic_reset_period": "2026-10-01", "traffic_reset_day": 1})
	db.Create(&model.InboundUserTraffic{InboundId: inbound.Id, ClientId: id, Up: 100})
	if _, err := s.SyncMonthlyTrafficPeriod(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetUserPolicies(inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Users[0].Up != 0 || view.Users[0].Total != 100 || view.Users[0].ExpiryTime != 1 || view.Users[0].EffectiveEnabled || view.Users[0].Period != "2026-11-01" {
		t.Fatalf("cycle changed allowance/expiry: %#v", view.Users[0])
	}
}

func TestIndividualQuotaCycleRestoresExhaustionWithoutEnablingManualDisable(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	s := InboundService{}
	db := database.GetDB()
	first, second := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	if err := db.Model(inbound).Updates(map[string]interface{}{"traffic_reset_period": "2026-10-01", "traffic_reset_day": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ?", inbound.Id).Update("total", 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.InboundUserPolicy{}).Where("inbound_id = ? AND client_id = ?", inbound.Id, second).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.AddTraffic([]*xray.Traffic{
		{IsUser: true, UserTag: inbound.Tag, ClientID: first, Up: 100},
		{IsUser: true, UserTag: inbound.Tag, ClientID: second, Up: 100},
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetUserPolicies(inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Users[0].EffectiveEnabled || before.Users[1].EffectiveEnabled || inbound.TrafficExhausted || !inbound.Enable {
		t.Fatal("fixture does not isolate personal exhaustion from parent inbound state")
	}
	restart, err := s.SyncMonthlyTrafficPeriod(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !restart {
		t.Fatal("personal quota reset did not request immediate core refresh")
	}
	view, err := s.GetUserPolicies(inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Users[0].EffectiveEnabled || view.Users[1].EffectiveEnabled || view.Users[1].Enabled || view.Users[0].Up != 0 || view.Users[1].Up != 0 || view.Users[0].Total != 100 {
		t.Fatalf("cycle did not restore only exhausted account: %#v", view.Users)
	}
	effective, err := s.effectiveInbound(inbound, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var settings vlessInboundSettings
	if err := json.Unmarshal([]byte(effective.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Clients) != 1 || settings.Clients[0].ID != first {
		t.Fatal("new core configuration did not restore only exhausted account")
	}
}
