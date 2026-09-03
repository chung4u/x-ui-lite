package service

import (
	"path/filepath"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

func TestDecideInboundTrafficPeriod(t *testing.T) {
	tests := []struct {
		name            string
		savedPeriod     string
		savedResetDay   int
		currentPeriod   string
		currentResetDay int
		want            inboundTrafficPeriodAction
	}{
		{name: "first initialization preserves counters", currentPeriod: "2026-09-01", currentResetDay: 1, want: inboundTrafficPeriodInitialize},
		{name: "reset day change rebases without clearing", savedPeriod: "2026-09-01", savedResetDay: 1, currentPeriod: "2026-09-15", currentResetDay: 15, want: inboundTrafficPeriodRebase},
		{name: "same cycle is unchanged", savedPeriod: "2026-09-15", savedResetDay: 15, currentPeriod: "2026-09-15", currentResetDay: 15, want: inboundTrafficPeriodUnchanged},
		{name: "next cycle resets counters", savedPeriod: "2026-08-15", savedResetDay: 15, currentPeriod: "2026-09-15", currentResetDay: 15, want: inboundTrafficPeriodReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideInboundTrafficPeriod(tt.savedPeriod, tt.savedResetDay, tt.currentPeriod, tt.currentResetDay)
			if got != tt.want {
				t.Fatalf("decideInboundTrafficPeriod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddTrafficPersistsUserTraffic(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(databasePath); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	inbound := &model.Inbound{UserId: 1, Port: 22002, Tag: "inbound-22002", Protocol: model.VLESS}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	service := InboundService{}
	if err := service.AddTraffic([]*xray.Traffic{
		{IsInbound: true, Tag: inbound.Tag, Up: 10, Down: 20},
		{IsUser: true, UserTag: inbound.Tag, ClientID: "user-a", Up: 7, Down: 11},
	}); err != nil {
		t.Fatalf("AddTraffic() error = %v", err)
	}
	traffics, err := service.GetInboundUserTraffic(inbound.Id)
	if err != nil || len(traffics) != 1 {
		t.Fatalf("GetInboundUserTraffic() = %#v, %v", traffics, err)
	}
	if traffics[0].ClientId != "user-a" || traffics[0].Up != 7 || traffics[0].Down != 11 {
		t.Fatalf("unexpected user traffic: %#v", traffics[0])
	}
}

func TestValidateInboundFlow(t *testing.T) {
	tests := []struct {
		name string
		flow string
		want bool
	}{
		{name: "empty flow", flow: "", want: true},
		{name: "vision flow", flow: "xtls-rprx-vision", want: true},
		{name: "legacy origin flow", flow: "xtls-rprx-origin", want: false},
		{name: "legacy direct flow", flow: "xtls-rprx-direct", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Protocol: model.VLESS,
				Settings: `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","flow":"` + tt.flow + `"}]}`,
			}
			err := validateInboundFlow(inbound)
			if (err == nil) != tt.want {
				t.Fatalf("validateInboundFlow() error = %v, want success = %v", err, tt.want)
			}
		})
	}
}

func TestSyncMonthlyTrafficPeriod(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(databasePath); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	settingService := SettingService{}
	if err := settingService.setInt("trafficResetDay", 15); err != nil {
		t.Fatalf("set traffic reset day: %v", err)
	}

	inbounds := []*model.Inbound{
		{UserId: 1, Port: 21001, Tag: "inbound-21001", Up: 10, Down: 20, Enable: true},
		{UserId: 1, Port: 21002, Tag: "inbound-21002", Up: 30, Down: 40, Enable: true, TrafficResetPeriod: "2026-09-01", TrafficResetDay: 1},
		{UserId: 1, Port: 21003, Tag: "inbound-21003", Up: 50, Down: 60, Enable: false, TrafficResetPeriod: "2026-08-15", TrafficResetDay: 15, TrafficExhausted: true},
		{UserId: 1, Port: 21004, Tag: "inbound-21004", Up: 70, Down: 80, Enable: false, ExpiryTime: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC).Unix() * 1000, TrafficResetPeriod: "2026-08-15", TrafficResetDay: 15, TrafficExhausted: true},
	}
	if err := database.GetDB().Create(inbounds).Error; err != nil {
		t.Fatalf("create inbounds: %v", err)
	}
	userTraffic := &model.InboundUserTraffic{InboundId: inbounds[2].Id, ClientId: "user-cycle", Up: 12, Down: 34}
	if err := database.GetDB().Create(userTraffic).Error; err != nil {
		t.Fatalf("create user traffic: %v", err)
	}

	service := InboundService{}
	needsRestart, err := service.SyncMonthlyTrafficPeriod(time.Date(2026, time.September, 15, 1, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("SyncMonthlyTrafficPeriod() error = %v", err)
	}
	if !needsRestart {
		t.Fatal("SyncMonthlyTrafficPeriod() should request an Xray restart when an exhausted inbound is restored")
	}

	var got []*model.Inbound
	if err := database.GetDB().Order("port asc").Find(&got).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if got[0].Up != 10 || got[0].Down != 20 || got[0].TrafficResetPeriod != "2026-09-15" {
		t.Fatalf("initialization should preserve counters: %#v", got[0])
	}
	if got[1].Up != 30 || got[1].Down != 40 || got[1].TrafficResetPeriod != "2026-09-15" || got[1].TrafficResetDay != 15 {
		t.Fatalf("reset day change should preserve and rebase counters: %#v", got[1])
	}
	if got[2].Up != 0 || got[2].Down != 0 || !got[2].Enable || got[2].TrafficExhausted {
		t.Fatalf("advanced cycle should reset and restore quota-disabled inbound: %#v", got[2])
	}
	if got[3].Up != 0 || got[3].Down != 0 || got[3].Enable || got[3].TrafficExhausted {
		t.Fatalf("expired inbound should stay disabled after cycle reset: %#v", got[3])
	}
	var resetUserTraffic model.InboundUserTraffic
	if err := database.GetDB().Where("inbound_id = ?", inbounds[2].Id).First(&resetUserTraffic).Error; err != nil {
		t.Fatalf("load user traffic: %v", err)
	}
	if resetUserTraffic.Up != 0 || resetUserTraffic.Down != 0 {
		t.Fatalf("advanced cycle should reset user traffic: %#v", resetUserTraffic)
	}
}

func TestUpdateInboundManualTrafficResetClearsExhaustion(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(databasePath); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	original := &model.Inbound{
		UserId:             1,
		Port:               22001,
		Tag:                "inbound-22001",
		Up:                 100,
		Down:               200,
		Total:              1000,
		Remark:             "manual reset",
		Enable:             false,
		TrafficResetPeriod: "2026-09-01",
		TrafficResetDay:    1,
		TrafficExhausted:   true,
	}
	if err := database.GetDB().Create(original).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	service := InboundService{}
	err := service.UpdateInbound(&model.Inbound{
		Id:             original.Id,
		Up:             0,
		Down:           0,
		Total:          original.Total,
		Remark:         original.Remark,
		Enable:         original.Enable,
		ExpiryTime:     original.ExpiryTime,
		Listen:         original.Listen,
		Port:           original.Port,
		Protocol:       original.Protocol,
		Settings:       original.Settings,
		StreamSettings: original.StreamSettings,
		Sniffing:       original.Sniffing,
	})
	if err != nil {
		t.Fatalf("UpdateInbound() error = %v", err)
	}
	updated, err := service.GetInbound(original.Id)
	if err != nil {
		t.Fatalf("GetInbound() error = %v", err)
	}
	if updated.Up != 0 || updated.Down != 0 || updated.TrafficExhausted {
		t.Fatalf("manual reset should clear exhaustion state: %#v", updated)
	}
}
