package service

import (
	"testing"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

func TestStopPersistsPendingTrafficOnceAfterCoreExit(t *testing.T) {
	policyTestDB(t)
	inbound := policyTestInbound(t)
	oldProcess, oldPending := p, pendingTraffic
	p = nil
	pendingTraffic = []*xray.Traffic{
		{IsInbound: true, Tag: inbound.Tag, Up: 13, Down: 29},
		{IsUser: true, UserTag: inbound.Tag, ClientID: "11111111-1111-1111-1111-111111111111", Up: 13, Down: 29},
	}
	defer func() { p, pendingTraffic = oldProcess, oldPending }()
	db := database.GetDB()
	if err := db.Exec("CREATE TRIGGER deny_accounting BEFORE UPDATE ON inbounds BEGIN SELECT RAISE(ABORT, 'simulated disk failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	s := &XrayService{}
	if err := s.StopXray(); err == nil {
		t.Fatal("accounting failure was ignored")
	}
	if len(pendingTraffic) != 2 {
		t.Fatal("pending deltas were discarded")
	}
	if err := db.Exec("DROP TRIGGER deny_accounting").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.StopXray(); err != nil {
		t.Fatal(err)
	}
	if len(pendingTraffic) != 0 {
		t.Fatal("pending deltas not cleared after commit")
	}
	if err := s.StopXray(); err != nil {
		t.Fatal(err)
	}
	var saved model.Inbound
	if err := db.First(&saved, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Up != 13 || saved.Down != 29 {
		t.Fatalf("delta duplicated or lost: up=%d down=%d", saved.Up, saved.Down)
	}
	rows, err := (&InboundService{}).GetInboundUserTraffic(inbound.Id)
	if err != nil || len(rows) != 1 || rows[0].Up != 13 || rows[0].Down != 29 {
		t.Fatalf("user deltas=%v error=%v", rows, err)
	}
}
