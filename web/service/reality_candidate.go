package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// Only these fixed destinations may be probed. Browser input never becomes a
// hostname or port, and DNS answers are checked before dialing their pinned IP.
var realityCandidateHosts = map[string]string{
	"mozilla":    "addons.mozilla.org",
	"cloudflare": "www.cloudflare.com",
	"speedtest":  "www.speedtest.net",
	"nvidia":     "www.nvidia.com",
	"amazon":     "www.amazon.com",
}

type RealityCandidateStatus struct {
	Candidate string `json:"candidate"`
	State     string `json:"state"`
	Message   string `json:"message"`
	CheckedAt int64  `json:"checkedAt"`
}

type realityCandidateResolver func(context.Context, string) ([]net.IPAddr, error)
type realityCandidateHandshake func(context.Context, string, string) (tls.ConnectionState, error)

func CheckRealityCandidate(ctx context.Context, candidate string) (*RealityCandidateStatus, error) {
	return checkRealityCandidate(ctx, candidate, net.DefaultResolver.LookupIPAddr, handshakeRealityCandidate)
}

func checkRealityCandidate(ctx context.Context, candidate string, resolve realityCandidateResolver, handshake realityCandidateHandshake) (*RealityCandidateStatus, error) {
	host, ok := realityCandidateHosts[candidate]
	if !ok {
		return nil, fmt.Errorf("请选择允许的 REALITY 候选站点")
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	result := &RealityCandidateStatus{Candidate: candidate, State: "unavailable", Message: "服务器暂时无法完成检测，请稍后重试或选择其他候选站点", CheckedAt: time.Now().UnixMilli()}
	addresses, err := resolve(ctx, host)
	if err != nil || len(addresses) == 0 {
		return result, nil
	}
	for _, address := range addresses {
		if !isPublicRealityCandidateIP(address.IP) {
			result.Message = "候选站点解析到非公网地址，已停止检测"
			return result, nil
		}
	}
	// Fixed-IP dialing deliberately avoids another DNS lookup. Race a bounded
	// set of both address families so black-holed IPv6 cannot consume the whole
	// request deadline before a healthy IPv4 address is attempted (or vice versa).
	jobs := make(chan net.IPAddr, len(addresses))
	for _, address := range interleaveRealityAddresses(addresses) {
		jobs <- address
	}
	close(jobs)
	type attempt struct {
		state tls.ConnectionState
		err   error
	}
	completed := make(chan attempt, len(addresses))
	for worker := 0; worker < 4 && worker < len(addresses); worker++ {
		go func() {
			for address := range jobs {
				if ctx.Err() != nil {
					return
				}
				state, err := handshake(ctx, net.JoinHostPort(address.IP.String(), "443"), host)
				select {
				case completed <- attempt{state, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	for remaining := len(addresses); remaining > 0; remaining-- {
		select {
		case <-ctx.Done():
			return result, nil
		case current := <-completed:
			if current.err != nil {
				continue
			}
			state := current.state
			if state.Version != tls.VersionTLS13 || state.NegotiatedProtocol != "h2" || len(state.VerifiedChains) == 0 {
				result.Message = "候选站点未同时通过 TLS 1.3、HTTP/2 与 SNI 证书校验，请选择其他候选站点"
				continue
			}
			result.State = "valid"
			result.Message = "服务器检测通过，可推荐使用 · TLS 1.3 / HTTP/2 / SNI 证书有效"
			return result, nil
		}
	}
	return result, nil
}

func interleaveRealityAddresses(addresses []net.IPAddr) []net.IPAddr {
	var ipv4, ipv6 []net.IPAddr
	for _, address := range addresses {
		if address.IP.To4() != nil {
			ipv4 = append(ipv4, address)
		} else {
			ipv6 = append(ipv6, address)
		}
	}
	ordered := make([]net.IPAddr, 0, len(addresses))
	for index := 0; index < len(ipv4) || index < len(ipv6); index++ {
		if index < len(ipv4) {
			ordered = append(ordered, ipv4[index])
		}
		if index < len(ipv6) {
			ordered = append(ordered, ipv6[index])
		}
	}
	return ordered
}

func realityCandidateTLSConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2"}}
}

func handshakeRealityCandidate(ctx context.Context, address, host string) (tls.ConnectionState, error) {
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	defer connection.Close()
	secure := tls.Client(connection, realityCandidateTLSConfig(host))
	if err := secure.HandshakeContext(ctx); err != nil {
		return tls.ConnectionState{}, err
	}
	return secure.ConnectionState(), nil
}

func isPublicRealityCandidateIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.To4() == nil {
		// IPv6 probes must use global unicast space; this excludes NAT64
		// synthesis that could map an apparently global address to private IPv4.
		_, global, _ := net.ParseCIDR("2000::/3")
		if !global.Contains(ip) {
			return false
		}
	}
	// CGNAT, benchmarking, documentation and reserved ranges are not destinations.
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
