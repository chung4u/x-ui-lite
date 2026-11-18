package service

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
)

func TestRuntimeTrendPersistenceAndWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trend.db")
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	samples := []model.RuntimeSample{{Time: now - int64((15*24*time.Hour)/time.Second), CPU: 1}, {Time: now - 600, CPU: 20, CPUPeak: 45, Memory: 60, State: "running"}, {Time: now - 300, CPU: 30, State: "error"}}
	if err := database.GetDB().Create(&samples).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.GetDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	rows, err := RuntimeTrend()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].CPU != 20 || rows[0].CPUPeak != 45 || rows[1].State != "error" {
		t.Fatalf("unexpected persisted series: %+v", rows)
	}
}

func TestDashboardTrendSummaryAndCursor(t *testing.T) {
	policyTestDB(t)
	now := time.Now()
	samples := []model.RuntimeSample{{Time: now.Add(-15 * 24 * time.Hour).Unix(), State: "error"}, {Time: now.Add(-time.Hour).Unix(), State: "error", CPUPeak: 80}, {Time: now.Unix(), State: "running", CPU: 20}}
	if err := database.GetDB().Create(&samples).Error; err != nil {
		t.Fatal(err)
	}
	b, err := RuntimeTrendSummary(now)
	if err != nil || len(b) != 1 || b[0].Bucket != 55 || b[0].Samples != 2 || b[0].Abnormal != 1 || b[0].Peak != 80 {
		t.Fatalf("summary %+v %v", b, err)
	}
	rows, err := RuntimeTrendSince(samples[1].Time)
	if err != nil || len(rows) != 1 || rows[0].Time != now.Unix() {
		t.Fatalf("cursor %+v %v", rows, err)
	}
}

func TestInboundDashboardHealthScope(t *testing.T) {
	policyTestDB(t)
	now := time.Now()
	for i, rec := range []struct {
		user                   int
		archived, expiry, used int64
		enabled                bool
	}{
		{1, 0, now.Add(time.Hour).UnixMilli(), 95, true},
		{1, 0, now.UnixMilli(), 100, false},
		{2, 0, 0, 99, true},
		{1, 1, 0, 99, true},
	} {
		row := model.Inbound{UserId: rec.user, Port: 3000 + i, Tag: fmt.Sprint("sample", i), ArchivedAt: rec.archived, ExpiryTime: rec.expiry, Up: rec.used, Total: 100, Enable: rec.enabled}
		if err := database.GetDB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	health, err := InboundHealthSummary(1, now)
	if err != nil || health != (InboundHealth{Total: 2, Enabled: 1, Disabled: 1, Expired: 1, Expiring: 1, Exhausted: 1, HighUsage: 1}) {
		t.Fatalf("summary %+v %v", health, err)
	}
}
