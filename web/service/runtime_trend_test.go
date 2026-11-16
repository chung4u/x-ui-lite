package service

import (
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
