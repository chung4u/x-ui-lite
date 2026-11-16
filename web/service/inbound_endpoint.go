package service

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// InboundEndpointStatus reports certificate validation only. It does not claim
// that a proxy request, credential, or backend route is healthy.
type InboundEndpointStatus struct {
	State         string `json:"state"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	NotAfter      int64  `json:"notAfter,omitempty"`
	DaysRemaining *int   `json:"daysRemaining,omitempty"`
	ALPN          string `json:"alpn"`
	CheckedAt     int64  `json:"checkedAt"`
	Message       string `json:"message"`
}

func (s *InboundService) GetEndpointStatus(inboundID int) (*InboundEndpointStatus, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return nil, err
	}
	if inbound.ArchivedAt != 0 || inbound.ExternalHost == "" || !inbound.ExternalTLS || inbound.ExternalPort < 1 || inbound.ExternalPort > 65535 {
		return nil, fmt.Errorf("仅支持当前托管 TLS 入口的证书检查")
	}
	// nil RootCAs deliberately uses the operating system trust store. Request
	// input cannot set the destination, trust store, SNI, or verification flags.
	status := probeEndpointTLS(inbound.ExternalHost, inbound.ExternalPort, nil, 5*time.Second)
	return &status, nil
}

func probeEndpointTLS(host string, port int, roots *x509.CertPool, timeout time.Duration) InboundEndpointStatus {
	status := InboundEndpointStatus{State: "unreachable", Host: host, Port: port, CheckedAt: time.Now().UnixMilli(), Message: "无法连接或完成 TLS 握手，证书尚未验证"}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{
		ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12, NextProtos: []string{"h2"},
	})
	if err != nil {
		var verification *tls.CertificateVerificationError
		var unknown x509.UnknownAuthorityError
		var hostname x509.HostnameError
		var invalid x509.CertificateInvalidError
		if errors.As(err, &verification) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
			status.State = "invalid"
			status.Message = "证书校验失败：证书链、域名或有效期不符合要求"
		}
		return status
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		status.State = "invalid"
		status.Message = "证书未建立可信验证链"
		return status
	}
	cert := state.PeerCertificates[0]
	status.State = "valid"
	status.NotAfter = cert.NotAfter.UnixMilli()
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	status.DaysRemaining = &days
	status.ALPN = state.NegotiatedProtocol
	status.Message = "证书有效，已通过系统信任链、域名和有效期校验"
	if status.ALPN != "h2" {
		status.Message += "；此次未协商 HTTP/2"
	}
	return status
}
