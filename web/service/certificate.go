package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"x-ui/util/common"
)

var generatedCertificateDir = "/etc/x-ui/certs"

type CertificateInfo struct {
	Subject       string `json:"subject"`
	Issuer        string `json:"issuer"`
	NotBefore     string `json:"notBefore"`
	NotAfter      string `json:"notAfter"`
	DaysRemaining int    `json:"daysRemaining"`
	Fingerprint   string `json:"fingerprint"`
	Algorithm     string `json:"algorithm"`
	SelfSigned    bool   `json:"selfSigned"`
}

type GeneratedCertificate struct {
	CertFile string           `json:"certFile"`
	KeyFile  string           `json:"keyFile"`
	Info     *CertificateInfo `json:"info"`
}

func (s *SettingService) GetCertificateInfo(certFile string) (*CertificateInfo, error) {
	path := strings.TrimSpace(certFile)
	if path == "" {
		return nil, common.NewError("尚未配置证书")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, common.NewError("证书文件不是有效的 PEM 证书")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	days := int(time.Until(certificate.NotAfter).Hours() / 24)
	if time.Until(certificate.NotAfter) > 0 {
		days++
	}
	selfSigned := certificate.Subject.String() == certificate.Issuer.String()
	return &CertificateInfo{
		Subject:       certificate.Subject.CommonName,
		Issuer:        certificate.Issuer.CommonName,
		NotBefore:     certificate.NotBefore.Local().Format("2006-01-02"),
		NotAfter:      certificate.NotAfter.Local().Format("2006-01-02"),
		DaysRemaining: days,
		Fingerprint:   strings.ToUpper(hex.EncodeToString(fingerprint[:])),
		Algorithm:     certificate.PublicKeyAlgorithm.String(),
		SelfSigned:    selfSigned,
	}, nil
}

func (s *SettingService) GeneratePanelCertificate(commonName string, validDays int) (*GeneratedCertificate, error) {
	commonName = strings.TrimSpace(commonName)
	if commonName == "" || len(commonName) > 253 || strings.ContainsAny(commonName, "\r\n/\\") {
		return nil, common.NewError("证书名称无效")
	}
	if validDays < 1 || validDays > 825 {
		return nil, common.NewError("证书有效期必须在 1–825 天之间")
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(0, 0, validDays),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(commonName); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{commonName}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(generatedCertificateDir, 0700); err != nil {
		return nil, err
	}
	stamp := now.UTC().Format("20060102-150405")
	base := filepath.Join(generatedCertificateDir, "panel-"+stamp)
	certFile := base + ".crt"
	keyFile := base + ".key"
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		return nil, err
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}), 0600); err != nil {
		_ = os.Remove(certFile)
		return nil, err
	}
	info, err := s.GetCertificateInfo(certFile)
	if err != nil {
		return nil, err
	}
	return &GeneratedCertificate{CertFile: certFile, KeyFile: keyFile, Info: info}, nil
}
