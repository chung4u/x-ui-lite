package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"
)

func startEndpointTLSTestServer(t *testing.T) (string, int, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RoyLive endpoint test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "local endpoint"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2"}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				conn.(*tls.Conn).Handshake()
			}()
		}
	}()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	return "127.0.0.1", port, roots
}

func TestEndpointTLSValidatesTrustAndNegotiatesHTTP2(t *testing.T) {
	host, port, roots := startEndpointTLSTestServer(t)
	result := probeEndpointTLS(host, port, roots, time.Second)
	if result.State != "valid" || result.ALPN != "h2" || result.NotAfter <= time.Now().UnixMilli() || result.CheckedAt == 0 {
		t.Fatalf("trusted endpoint did not validate: %#v", result)
	}
}

func TestEndpointTLSRejectsUntrustedCertificate(t *testing.T) {
	host, port, _ := startEndpointTLSTestServer(t)
	result := probeEndpointTLS(host, port, x509.NewCertPool(), time.Second)
	if result.State != "invalid" || result.NotAfter != 0 || result.ALPN != "" {
		t.Fatalf("untrusted certificate must not report valid metadata: %#v", result)
	}
}

func TestEndpointTLSBoundsHandshakeAndDoesNotCallTimeoutInvalid(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-done
	}()
	defer close(done)
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	start := time.Now()
	result := probeEndpointTLS("127.0.0.1", port, nil, 100*time.Millisecond)
	if result.State != "unreachable" || result.NotAfter != 0 || time.Since(start) > time.Second {
		t.Fatalf("handshake timeout not bounded/classified: %#v", result)
	}
}
