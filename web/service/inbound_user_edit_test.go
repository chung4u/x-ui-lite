package service

import (
	"encoding/json"
	"testing"
	"x-ui/database"
	"x-ui/database/model"
)

func userEditBool(v bool) *bool    { return &v }
func userEditInt64(v int64) *int64 { return &v }
func TestManagedUserAtomicEditPreservesAccountingAndTokens(t *testing.T) {
	policyTestDB(t)
	in := policyTestInbound(t)
	s := InboundService{}
	db := database.GetDB()
	id := "11111111-1111-1111-1111-111111111111"
	group := model.InboundQuotaGroup{InboundId: in.Id, Name: "shared", Enabled: true, Total: 500, Up: 31, Down: 47}
	db.Create(&group)
	db.Create(&model.InboundUserTraffic{InboundId: in.Id, ClientId: id, Up: 11, Down: 23})
	// The service changes identity and individual allowance, never accounting or tokens.
	token := model.InboundSubscription{InboundId: in.Id, ClientId: id, Token: "stable-test-token"}
	db.Create(&token)
	beforeSettings := in.Settings
	bad := ManagedUserChange{ClientId: id, Email: "new", Enabled: userEditBool(false), GroupId: 999, Total: userEditInt64(100), ExpiryTime: userEditInt64(0), ExpectedEnabled: userEditBool(true), ExpectedTotal: userEditInt64(0), ExpectedExpiryTime: userEditInt64(0)}
	if s.SaveManagedUser(1, in.Id, &bad) == nil {
		t.Fatal("foreign group accepted")
	}
	db.First(in, in.Id)
	if in.Settings != beforeSettings {
		t.Fatal("partial name update")
	}
	bad.GroupId = 0
	if err := s.SaveManagedUser(1, in.Id, &bad); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetUserPolicies(in.Id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Users[0].Enabled || view.Users[0].GroupId != 0 || view.Users[0].Total != 100 || view.Users[0].Up != 11 || view.Users[0].Down != 23 || len(view.Groups) != 0 {
		t.Fatal("policy or traffic mismatch")
	}
	var savedToken model.InboundSubscription
	db.First(&savedToken, token.Id)
	if savedToken.Token != token.Token || savedToken.ClientId != id {
		t.Fatal("subscription changed")
	}
	db.First(&group, group.Id)
	if group.Up != 31 || group.Down != 47 {
		t.Fatal("group traffic changed")
	}
	if s.SaveManagedUser(1, in.Id, &bad) == nil {
		t.Fatal("stale edit accepted")
	}
	if s.SaveManagedUser(2, in.Id, &bad) == nil {
		t.Fatal("other owner accepted")
	}
	var state vlessInboundSettings
	db.First(in, in.Id)
	json.Unmarshal([]byte(in.Settings), &state)
	if state.Clients[0].Email != "new" || len(state.Clients) != 2 {
		t.Fatal("client update wrong")
	}
}
func TestManagedUserCreateDeleteAndReusedUUID(t *testing.T) {
	policyTestDB(t)
	in := policyTestInbound(t)
	s := InboundService{}
	change := ManagedUserChange{ClientId: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Email: "new", Enabled: userEditBool(false), Total: userEditInt64(0), ExpiryTime: userEditInt64(0), Create: true}
	if err := s.SaveManagedUser(1, in.Id, &change); err != nil {
		t.Fatal(err)
	}
	if s.SaveManagedUser(1, in.Id, &change) == nil {
		t.Fatal("duplicate create accepted")
	}
	view, _ := s.GetUserPolicies(in.Id)
	if len(view.Users) != 3 || view.Users[2].EffectiveEnabled {
		t.Fatal("new disabled user not atomic")
	}
	change.Create = false
	change.Delete = true
	change.ExpectedEnabled = userEditBool(false)
	change.ExpectedEmail = "new"
	change.ExpectedTotal = userEditInt64(0)
	change.ExpectedExpiryTime = userEditInt64(0)
	if err := s.SaveManagedUser(1, in.Id, &change); err != nil {
		t.Fatal(err)
	}
	view, _ = s.GetUserPolicies(in.Id)
	if len(view.Users) != 2 {
		t.Fatal("delete failed")
	}
	change.Create = true
	change.Delete = false
	change.ClientId = "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA"
	if s.SaveManagedUser(1, in.Id, &change) == nil {
		t.Fatal("deleted UUID reused with case change")
	}
	change.ClientId = "invalid"
	if s.SaveManagedUser(1, in.Id, &change) == nil {
		t.Fatal("invalid UUID accepted")
	}
	database.GetDB().Model(in).Update("archived_at", 1)
	change.ClientId = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	if s.SaveManagedUser(1, in.Id, &change) == nil {
		t.Fatal("archive edited")
	}
}
