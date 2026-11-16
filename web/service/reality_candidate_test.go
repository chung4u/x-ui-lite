package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealityCandidateOnlyProbesWhitelistAndPublicAddresses(t *testing.T) {
	lookupCalls, dialCalls := 0, 0
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		lookupCalls++
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}
	dial := func(context.Context, string, string) (tls.ConnectionState, error) {
		dialCalls++
		return tls.ConnectionState{}, nil
	}
	for _, input := range []string{"https://www.cloudflare.com", "127.0.0.1", "mozilla:80", "", "MOZILLA"} {
		if _, err := checkRealityCandidate(context.Background(), input, lookup, dial); err == nil {
			t.Fatalf("accepted arbitrary destination %q", input)
		}
	}
	if lookupCalls != 0 || dialCalls != 0 {
		t.Fatal("untrusted input reached network")
	}
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "172.18.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "::1", "fc00::1", "fe80::1", "2001:db8::1", "::ffff:127.0.0.1"} {
		lookup = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP(value)}}, nil
		}
		result, err := checkRealityCandidate(context.Background(), "mozilla", lookup, dial)
		if err != nil || result.State != "unavailable" || dialCalls != 0 {
			t.Fatalf("non-public DNS %s reached dial: %v", value, err)
		}
	}
}

func TestRealityCandidateHealthyFamilyBypassesBlackHoledOtherFamily(t *testing.T) {
	for _, healthyIPv4 := range []bool{true, false} {
		t.Run(fmt.Sprintf("healthyIPv4=%v", healthyIPv4), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Deliberately return several black-holed addresses before the healthy
			// family, as production DNS did when IPv6 was unusable.
			var addresses []net.IPAddr
			for n := 1; n <= 4; n++ {
				ip := net.ParseIP(fmt.Sprintf("2a04:4e42::%d", n))
				if !healthyIPv4 {
					ip = net.ParseIP(fmt.Sprintf("151.101.%d.91", n))
				}
				addresses = append(addresses, net.IPAddr{IP: ip})
			}
			healthy := net.ParseIP("1.1.1.1")
			if !healthyIPv4 {
				healthy = net.ParseIP("2606:4700::1111")
			}
			addresses = append(addresses, net.IPAddr{IP: healthy})
			lookup := func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil }
			var pending atomic.Int32
			handshake := func(ctx context.Context, address, hostname string) (tls.ConnectionState, error) {
				host, _, _ := net.SplitHostPort(address)
				if net.ParseIP(host).Equal(healthy) {
					return tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "h2", VerifiedChains: [][]*x509.Certificate{{{}}}}, nil
				}
				pending.Add(1)
				defer pending.Add(-1)
				<-ctx.Done()
				return tls.ConnectionState{}, ctx.Err()
			}
			done := make(chan *RealityCandidateStatus, 1)
			go func() { result, _ := checkRealityCandidate(ctx, "mozilla", lookup, handshake); done <- result }()
			select {
			case result := <-done:
				if result.State != "valid" {
					t.Fatalf("healthy address was blocked: %+v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("black-holed address family consumed the probe deadline")
			}
			deadline := time.Now().Add(time.Second)
			for pending.Load() != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if pending.Load() != 0 {
				t.Fatal("successful probe did not cancel losing handshakes")
			}
		})
	}
}

func TestRealityCandidateProbeConcurrencyBoundAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var addresses []net.IPAddr
	for n := 1; n <= 12; n++ {
		addresses = append(addresses, net.IPAddr{IP: net.ParseIP(fmt.Sprintf("151.101.%d.91", n))})
	}
	lookup := func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil }
	started := make(chan struct{}, len(addresses))
	var active, maximum atomic.Int32
	handshake := func(ctx context.Context, address, host string) (tls.ConnectionState, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); count > old && !maximum.CompareAndSwap(old, count); old = maximum.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return tls.ConnectionState{}, ctx.Err()
	}
	done := make(chan *RealityCandidateStatus, 1)
	go func() { result, _ := checkRealityCandidate(ctx, "mozilla", lookup, handshake); done <- result }()
	for n := 0; n < 4; n++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	if maximum.Load() > 4 {
		t.Fatalf("unbounded parallelism: %d", maximum.Load())
	}
	cancel()
	select {
	case result := <-done:
		if result.State != "unavailable" {
			t.Fatal("canceled probe reported valid")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled probe did not stop")
	}
	deadline := time.Now().Add(time.Second)
	for active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active.Load() != 0 || maximum.Load() > 4 {
		t.Fatal("probe workers leaked or exceeded concurrency limit")
	}
}

func TestRealityCandidateRequiresTLS13H2AndVerifiedCertificate(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host != "addons.mozilla.org" {
			t.Fatalf("wrong lookup host %q", host)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("probe has no bounded timeout")
		}
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}
	for _, tc := range []struct {
		name      string
		version   uint16
		alpn      string
		verified  bool
		dialError bool
		valid     bool
	}{
		{"valid", tls.VersionTLS13, "h2", true, false, true},
		{"TLS12", tls.VersionTLS12, "h2", true, false, false},
		{"HTTP11", tls.VersionTLS13, "http/1.1", true, false, false},
		{"unverified", tls.VersionTLS13, "h2", false, false, false},
		{"handshake failure", tls.VersionTLS13, "h2", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dial := func(ctx context.Context, address, hostname string) (tls.ConnectionState, error) {
				if address != "1.1.1.1:443" || hostname != "addons.mozilla.org" {
					t.Fatalf("DNS not pinned or SNI missing: %s %s", address, hostname)
				}
				if tc.dialError {
					return tls.ConnectionState{}, errors.New("TLS rejected")
				}
				state := tls.ConnectionState{Version: tc.version, NegotiatedProtocol: tc.alpn}
				if tc.verified {
					state.VerifiedChains = [][]*x509.Certificate{{{}}}
				}
				return state, nil
			}
			result, err := checkRealityCandidate(context.Background(), "mozilla", lookup, dial)
			if err != nil || (result.State == "valid") != tc.valid || result.CheckedAt == 0 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	config := realityCandidateTLSConfig("addons.mozilla.org")
	if config.InsecureSkipVerify || config.MinVersion != tls.VersionTLS13 || config.ServerName != "addons.mozilla.org" || len(config.NextProtos) != 1 || config.NextProtos[0] != "h2" {
		t.Fatal("unsafe probe TLS configuration")
	}
}
