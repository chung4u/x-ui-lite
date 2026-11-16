package database

import (
	"path/filepath"
	"testing"
	"x-ui/database/model"
)

func TestIndependentQuotaMigrationPreservesHistoryAndRunsOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "quota.db")
	if err := InitDB(file); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{UserId: 1, Protocol: model.VLESS, ExternalHost: "example.com", Settings: `{"clients":[{"id":"one"},{"id":"two"}]}`}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	group := model.InboundQuotaGroup{InboundId: inbound.Id, Name: "historical", Enabled: true, Total: 500, Up: 70, Down: 80, ExpiryTime: 1234567890000, ResetDay: 3, Period: "2026-10-03"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	policies := []model.InboundUserPolicy{
		{InboundId: inbound.Id, ClientId: "one", Enabled: true, GroupId: group.Id},
		{InboundId: inbound.Id, ClientId: "two", Enabled: false, GroupId: group.Id},
		{InboundId: inbound.Id, ClientId: "deleted", Enabled: true, GroupId: 9999},
	}
	if err := db.Create(&policies).Error; err != nil {
		t.Fatal(err)
	}
	traffic := model.InboundUserTraffic{InboundId: inbound.Id, ClientId: "one", Up: 11, Down: 23}
	token := model.InboundSubscription{InboundId: inbound.Id, ClientId: "one", Token: "stable"}
	db.Create(&traffic)
	db.Create(&token)
	if err := InitDB(file); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.First(&policies[i], policies[i].Id).Error; err != nil {
			t.Fatal(err)
		}
		if !policies[i].QuotaInitialized || policies[i].Total != 500 || policies[i].ExpiryTime != group.ExpiryTime || policies[i].GroupId != group.Id || policies[i].Enabled != (i == 0) {
			t.Fatalf("individual allowance migration wrong: %#v", policies[i])
		}
	}
	db.First(&traffic, traffic.Id)
	db.First(&group, group.Id)
	db.First(&token, token.Id)
	db.First(&policies[2], policies[2].Id)
	if traffic.Up != 11 || traffic.Down != 23 || group.Up != 70 || group.Down != 80 || token.Token != "stable" || policies[2].QuotaInitialized {
		t.Fatal("history, token or removed member changed")
	}
	db.Model(&policies[0]).Update("total", 900)
	if err := InitDB(file); err != nil {
		t.Fatal(err)
	}
	db.First(&policies[0], policies[0].Id)
	if policies[0].Total != 900 {
		t.Fatal("restart overwrote personal allowance")
	}
}

func TestIndependentQuotaMigrationRejectsMissingGroupAtomically(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "quota.db")); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Protocol: model.VLESS, ExternalHost: "example.com", Settings: `{"clients":[{"id":"one"},{"id":"two"}]}`}
	db.Create(&inbound)
	group := model.InboundQuotaGroup{InboundId: inbound.Id, Name: "disabled", Enabled: false, Total: 500}
	db.Create(&group)
	policies := []model.InboundUserPolicy{
		{InboundId: inbound.Id, ClientId: "one", Enabled: true, GroupId: group.Id},
		{InboundId: inbound.Id, ClientId: "two", Enabled: true, GroupId: 9999},
	}
	db.Create(&policies)
	if err := migrateManagedUserQuotas(); err == nil {
		t.Fatal("missing group widened access")
	}
	db.First(&policies[0], policies[0].Id)
	if policies[0].QuotaInitialized || !policies[0].Enabled {
		t.Fatal("failed migration partially committed")
	}
	db.Model(&policies[1]).Update("group_id", group.Id)
	if err := migrateManagedUserQuotas(); err != nil {
		t.Fatal(err)
	}
	db.First(&policies[0], policies[0].Id)
	if !policies[0].QuotaInitialized || policies[0].Enabled {
		t.Fatal("disabled group access was not retained")
	}
}
