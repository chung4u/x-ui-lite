package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Only a local process signal can prepare maintenance. No public HTTP endpoint
// or additional credential is exposed. The operator must first quiesce traffic.
func maintenanceReceiptPath() (string, error) {
	path := os.Getenv("XUI_MAINTENANCE_RECEIPT")
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("XUI_MAINTENANCE_RECEIPT must be an absolute path")
	}
	return path, nil
}

func writeMaintenanceReceipt(path string) error {
	data, err := json.Marshal(struct {
		PID        int   `json:"pid"`
		Prepared   bool  `json:"prepared"`
		PreparedAt int64 `json:"prepared_at"`
	}{os.Getpid(), true, time.Now().Unix()})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".maintenance-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
