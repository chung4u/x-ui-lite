package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceReceiptRequiresExplicitAbsolutePath(t *testing.T) {
	for _, p := range []string{"", "relative.json"} {
		t.Setenv("XUI_MAINTENANCE_RECEIPT", p)
		if _, err := maintenanceReceiptPath(); err == nil {
			t.Fatal("accepted unconfigured receipt path")
		}
	}
}

func TestMaintenanceReceiptIsPrivateAndIdentifiesProcess(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ready.json")
	if err := writeMaintenanceReceipt(p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		PID        int   `json:"pid"`
		Prepared   bool  `json:"prepared"`
		PreparedAt int64 `json:"prepared_at"`
	}
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PID != os.Getpid() || !receipt.Prepared || receipt.PreparedAt <= 0 {
		t.Fatalf("invalid receipt: %s", data)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("receipt mode=%v", info.Mode())
	}
}
