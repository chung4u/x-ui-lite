package service

import (
	"path/filepath"
	"testing"
)

func TestGeneratePanelCertificate(t *testing.T) {
	originalDir := generatedCertificateDir
	generatedCertificateDir = t.TempDir()
	defer func() { generatedCertificateDir = originalDir }()

	result, err := (&SettingService{}).GeneratePanelCertificate("panel.example.com", 90)
	if err != nil {
		t.Fatalf("GeneratePanelCertificate() error = %v", err)
	}
	if filepath.Dir(result.CertFile) != generatedCertificateDir || result.Info.Subject != "panel.example.com" {
		t.Fatalf("unexpected generated certificate: %#v", result)
	}
	if result.Info.DaysRemaining < 89 || result.Info.DaysRemaining > 91 || !result.Info.SelfSigned {
		t.Fatalf("unexpected certificate info: %#v", result.Info)
	}
}
