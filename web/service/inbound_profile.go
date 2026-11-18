package service

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
	"x-ui/database/model"
	"x-ui/util/common"

	"gopkg.in/yaml.v2"
)

type VLESSUserProfile struct {
	Link             string                 `json:"link"`
	ClashVergeConfig string                 `json:"clashVergeConfig"`
	FileName         string                 `json:"fileName"`
	Connection       VLESSConnectionDetails `json:"connection"`
}

// VLESSConnectionDetails contains only client-side connection fields. It is
// returned to the authenticated inbound owner so the panel can show the same
// values that are written to the downloadable Clash Verge profile.
type VLESSConnectionDetails struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Server            string   `json:"server"`
	Port              int      `json:"port"`
	UUID              string   `json:"uuid"`
	Encryption        string   `json:"encryption"`
	Network           string   `json:"network"`
	UDP               bool     `json:"udp"`
	TLS               bool     `json:"tls"`
	ServerName        string   `json:"serverName"`
	Flow              string   `json:"flow"`
	ClientFingerprint string   `json:"clientFingerprint"`
	RealityPublicKey  string   `json:"realityPublicKey"`
	XHTTPPath         string   `json:"xhttpPath,omitempty"`
	XHTTPMode         string   `json:"xhttpMode,omitempty"`
	XHTTPHost         string   `json:"xhttpHost,omitempty"`
	ALPN              []string `json:"alpn,omitempty"`
	SkipCertVerify    bool     `json:"skipCertVerify"`
}

type vlessInboundSettings struct {
	Clients []vlessInboundClient `json:"clients"`
}

type vlessInboundClient struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Flow  string `json:"flow"`
}

type vlessStreamSettings struct {
	Network     string `json:"network"`
	Security    string `json:"security"`
	TLSSettings struct {
		ServerName string `json:"serverName"`
	} `json:"tlsSettings"`
	RealitySettings struct {
		ServerNames       []string `json:"serverNames"`
		PrivateKey        string   `json:"privateKey"`
		ShortIDs          []string `json:"shortIds"`
		ClientFingerprint string   `json:"clientFingerprint"`
	} `json:"realitySettings"`
	WSSettings struct {
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
	} `json:"wsSettings"`
	HTTPSettings struct {
		Path string   `json:"path"`
		Host []string `json:"host"`
	} `json:"httpSettings"`
	GRPCSettings struct {
		ServiceName string `json:"serviceName"`
	} `json:"grpcSettings"`
	XHTTPSettings struct {
		Path string `json:"path"`
		Host string `json:"host"`
		Mode string `json:"mode"`
	} `json:"xhttpSettings"`
}

type clashVLESSConfig struct {
	DNS         map[string]interface{} `yaml:"dns"`
	MixedPort   int                    `yaml:"mixed-port"`
	Mode        string                 `yaml:"mode"`
	LogLevel    string                 `yaml:"log-level"`
	Proxies     []clashVLESSProxy      `yaml:"proxies"`
	ProxyGroups []clashProxyGroup      `yaml:"proxy-groups"`
	Rules       []string               `yaml:"rules"`
}

type clashVLESSProxy struct {
	Name              string            `yaml:"name"`
	Type              string            `yaml:"type"`
	Server            string            `yaml:"server"`
	Port              int               `yaml:"port"`
	Password          string            `yaml:"password,omitempty"`
	SNI               string            `yaml:"sni,omitempty"`
	UUID              string            `yaml:"uuid,omitempty"`
	Flow              string            `yaml:"flow,omitempty"`
	Encryption        string            `yaml:"encryption,omitempty"`
	Network           string            `yaml:"network,omitempty"`
	TLS               bool              `yaml:"tls,omitempty"`
	ServerName        string            `yaml:"servername,omitempty"`
	ClientFingerprint string            `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *clashRealityOpts `yaml:"reality-opts,omitempty"`
	WSOpts            *clashWSOpts      `yaml:"ws-opts,omitempty"`
	H2Opts            *clashH2Opts      `yaml:"h2-opts,omitempty"`
	GRPCOpts          *clashGRPCOpts    `yaml:"grpc-opts,omitempty"`
	XHTTPOpts         *clashXHTTPOpts   `yaml:"xhttp-opts,omitempty"`
	ALPN              []string          `yaml:"alpn,omitempty"`
	SkipCertVerify    *bool             `yaml:"skip-cert-verify,omitempty"`
	UDP               bool              `yaml:"udp"`
}

type clashXHTTPOpts struct {
	Path          string                  `yaml:"path"`
	Host          string                  `yaml:"host"`
	Mode          string                  `yaml:"mode"`
	ReuseSettings clashXHTTPReuseSettings `yaml:"reuse-settings"`
}

type clashXHTTPReuseSettings struct {
	MaxConnections   string `yaml:"max-connections" json:"maxConnections"`
	HMaxRequestTimes string `yaml:"h-max-request-times" json:"hMaxRequestTimes"`
	HMaxReusableSecs string `yaml:"h-max-reusable-secs" json:"hMaxReusableSecs"`
}

type clashRealityOpts struct {
	PublicKey             string `yaml:"public-key"`
	ShortID               string `yaml:"short-id,omitempty"`
	SupportX25519MLKEM768 bool   `yaml:"support-x25519mlkem768"`
}

type clashWSOpts struct {
	Path    string            `yaml:"path,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type clashH2Opts struct {
	Path string   `yaml:"path,omitempty"`
	Host []string `yaml:"host,omitempty"`
}

type clashGRPCOpts struct {
	GRPCServiceName string `yaml:"grpc-service-name,omitempty"`
}

type clashProxyGroup struct {
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
	Timeout  int      `yaml:"timeout,omitempty"`
	Lazy     *bool    `yaml:"lazy,omitempty"`
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
}

func (s *InboundService) GetVLESSUserProfile(inboundID int, userID int, clientID string, host string, requestedName string) (*VLESSUserProfile, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return nil, err
	}
	if inbound.UserId != userID {
		return nil, common.NewError("无权访问该入站")
	}
	if err := ValidateInboundUserAccess(inbound, clientID); err != nil {
		return nil, err
	}
	return buildVLESSUserProfile(inbound, clientID, host, requestedName)
}

func buildVLESSUserProfile(inbound *model.Inbound, clientID string, host string, requestedName string) (*VLESSUserProfile, error) {
	if inbound.Protocol != model.VLESS {
		return nil, common.NewError("仅支持 VLESS 入站")
	}
	if inbound.ArchivedAt > 0 {
		return nil, common.NewError("该入站已归档")
	}

	settings := vlessInboundSettings{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, common.NewError("VLESS 用户配置无效:", err)
	}
	var client *vlessInboundClient
	for index := range settings.Clients {
		if settings.Clients[index].ID == clientID {
			client = &settings.Clients[index]
			break
		}
	}
	if client == nil || strings.TrimSpace(client.ID) == "" {
		return nil, common.NewError("未找到指定 VLESS 用户")
	}

	stream := vlessStreamSettings{}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
		return nil, common.NewError("传输配置无效:", err)
	}
	if stream.Network == "xhttp" {
		if err := validateManagedXHTTPProfile(inbound, client, stream); err != nil {
			return nil, err
		}
		host = inbound.ExternalHost
	} else if inbound.ExternalHost != "" || inbound.ExternalPort != 0 || inbound.ExternalTLS {
		if err := validateForwardedRealityEndpoint(inbound); err != nil {
			return nil, err
		}
		host = inbound.ExternalHost
	}
	server, err := normalizeProfileHost(host)
	if err != nil {
		return nil, err
	}
	name := "US｜" + firstNonEmpty([]string{client.Email, requestedName, "用户"})

	link, proxy, err := buildVLESSProfile(inbound, client, stream, server, name)
	if err != nil {
		return nil, err
	}
	dns := map[string]interface{}{}
	if err := json.Unmarshal([]byte(`{"enable":true,"use-hosts":true,"enhanced-mode":"redir-host","default-nameserver":["223.5.5.5","119.29.29.29"],"nameserver":["https://dns.alidns.com/dns-query","https://doh.pub/dns-query"],"proxy-server-nameserver":["https://dns.alidns.com/dns-query","https://doh.pub/dns-query"],"nameserver-policy":{"geosite:cn":["https://dns.alidns.com/dns-query","https://doh.pub/dns-query"]},"fallback":["https://1.1.1.1/dns-query#PROXY","https://8.8.8.8/dns-query#PROXY"],"fallback-filter":{"geoip":true,"geoip-code":"CN","ipcidr":["240.0.0.0/4","0.0.0.0/32","127.0.0.1/32"],"domain":["+.google.com","+.youtube.com","+.facebook.com","+.openai.com","+.chatgpt.com"]}}`), &dns); err != nil {
		return nil, err
	}
	config := clashVLESSConfig{
		DNS:       dns,
		MixedPort: 7890,
		Mode:      "rule",
		LogLevel:  "info",
		Proxies:   []clashVLESSProxy{proxy},
		ProxyGroups: []clashProxyGroup{{
			Name:    "PROXY",
			Type:    "select",
			Proxies: []string{name, "DIRECT"},
		}, {Name: "广告过滤", Type: "select", Proxies: []string{"DIRECT", "REJECT"}}},
		Rules: []string{directProfileRule(server), "DOMAIN-SUFFIX,localhost,DIRECT", "DOMAIN-SUFFIX,local,DIRECT", "DOMAIN-SUFFIX,lan,DIRECT", "GEOSITE,category-ads-all,广告过滤", "DOMAIN-SUFFIX,cn,DIRECT", "GEOIP,LAN,DIRECT", "GEOSITE,cn,DIRECT", "GEOIP,CN,DIRECT", "MATCH,PROXY"},
	}
	if err := applySubscriptionPolicy(&config); err != nil {
		return nil, err
	}
	hy, err := hyProxy(inbound.Id, clientID)
	if err != nil {
		return nil, err
	}
	if hy != nil {
		if hy.Server != server {
			config.Rules = append([]string{directProfileRule(hy.Server)}, config.Rules...)
		}
		if hy.Name == name {
			hy.Name += " (HY2)"
		}
		config.Proxies = append(config.Proxies, *hy)
		auto := "自动（HY2 优先）"
		if auto == name {
			auto += " · 自动"
		}
		lazy := false
		config.ProxyGroups = append(config.ProxyGroups, clashProxyGroup{
			Name: auto, Type: "fallback", Proxies: []string{hy.Name, name},
			URL: "https://www.gstatic.com/generate_204", Interval: 60, Timeout: 5000, Lazy: &lazy,
		})
		config.ProxyGroups[0].Proxies = []string{auto, hy.Name, name, "DIRECT"}
	}
	content, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	return &VLESSUserProfile{
		Link:             link,
		ClashVergeConfig: string(content),
		FileName:         safeProfileFileName(name) + ".yaml",
		Connection:       connectionDetailsFromProxy(proxy),
	}, nil
}

// An installation can keep its existing client DNS and routing policy when
// moving from a locally branded build to the shared release binary.
func applySubscriptionPolicy(config *clashVLESSConfig) error {
	path := os.Getenv("XUI_SUBSCRIPTION_POLICY_FILE")
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取订阅策略失败: %w", err)
	}
	var policy struct {
		DNS   map[string]interface{} `json:"dns"`
		Rules []string               `json:"rules"`
	}
	if err = json.Unmarshal(data, &policy); err != nil {
		return fmt.Errorf("解析订阅策略失败: %w", err)
	}
	if len(policy.DNS) == 0 || len(policy.Rules) == 0 {
		return fmt.Errorf("订阅策略需包含 DNS 和规则")
	}
	config.DNS, config.Rules = policy.DNS, policy.Rules
	config.ProxyGroups = config.ProxyGroups[:1]
	return nil
}

func connectionDetailsFromProxy(proxy clashVLESSProxy) VLESSConnectionDetails {
	details := VLESSConnectionDetails{
		Name:              proxy.Name,
		Type:              proxy.Type,
		Server:            proxy.Server,
		Port:              proxy.Port,
		UUID:              proxy.UUID,
		Encryption:        proxy.Encryption,
		Network:           proxy.Network,
		UDP:               proxy.UDP,
		TLS:               proxy.TLS,
		ServerName:        proxy.ServerName,
		Flow:              proxy.Flow,
		ClientFingerprint: proxy.ClientFingerprint,
	}
	if proxy.RealityOpts != nil {
		details.RealityPublicKey = proxy.RealityOpts.PublicKey
	}
	if proxy.XHTTPOpts != nil {
		details.XHTTPPath = proxy.XHTTPOpts.Path
		details.XHTTPMode = proxy.XHTTPOpts.Mode
		details.XHTTPHost = proxy.XHTTPOpts.Host
		details.ALPN = append([]string(nil), proxy.ALPN...)
	}
	if proxy.SkipCertVerify != nil {
		details.SkipCertVerify = *proxy.SkipCertVerify
	}
	return details
}

// The panel owns an HTTP backend, while clients connect to Caddy's public TLS
// endpoint. Never infer the public port or TLS mode from the backend socket.
func validateManagedXHTTPProfile(inbound *model.Inbound, client *vlessInboundClient, stream vlessStreamSettings) error {
	host := strings.TrimSpace(inbound.ExternalHost)
	if host == "" || strings.ContainsAny(host, " \t\r\n/\\?#@") ||
		(strings.Contains(host, ":") && net.ParseIP(host) == nil) || strings.ContainsAny(host, "[]") {
		return common.NewError("XHTTP 公网域名配置无效")
	}
	if inbound.ExternalPort < 1 || inbound.ExternalPort > 65535 || !inbound.ExternalTLS {
		return common.NewError("XHTTP 公网 TLS 入口配置不完整")
	}
	if stream.Security != "none" && stream.Security != "" {
		return common.NewError("托管 XHTTP 的 TLS 应由外部入口终止")
	}
	if client.Flow != "" {
		return common.NewError("XHTTP 用户 Flow 必须为空")
	}
	path := stream.XHTTPSettings.Path
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n?#") {
		return common.NewError("XHTTP 路径配置无效")
	}
	if configured := strings.TrimSpace(stream.XHTTPSettings.Host); configured != "" && !strings.EqualFold(configured, host) {
		return common.NewError("XHTTP Host 与公网域名不一致")
	}
	switch stream.XHTTPSettings.Mode {
	case "", "auto", "stream-up", "stream-one", "packet-up":
	default:
		return common.NewError("XHTTP 模式无效")
	}
	return nil
}

func buildVLESSProfile(inbound *model.Inbound, client *vlessInboundClient, stream vlessStreamSettings, server string, name string) (string, clashVLESSProxy, error) {
	network := stream.Network
	if network == "" {
		network = "tcp"
	}
	proxy := clashVLESSProxy{
		Name:       name,
		Type:       "vless",
		Server:     server,
		Port:       inbound.Port,
		UUID:       client.ID,
		Flow:       client.Flow,
		Encryption: "none",
		Network:    network,
		UDP:        true,
	}
	params := url.Values{}
	params.Set("encryption", "none")
	params.Set("type", network)

	security := strings.TrimSpace(stream.Security)
	switch security {
	case "reality":
		if inbound.ExternalPort > 0 {
			proxy.Port = inbound.ExternalPort
		}
		serverName := firstNonEmpty(stream.RealitySettings.ServerNames)
		publicKey, err := deriveRealityPublicKey(stream.RealitySettings.PrivateKey)
		if err != nil {
			return "", proxy, err
		}
		shortID := firstNonEmpty(stream.RealitySettings.ShortIDs)
		proxy.TLS = true
		proxy.ServerName = serverName
		fingerprint := strings.TrimSpace(stream.RealitySettings.ClientFingerprint)
		if fingerprint == "" {
			fingerprint = "chrome"
		}
		proxy.ClientFingerprint = fingerprint
		proxy.RealityOpts = &clashRealityOpts{
			PublicKey:             publicKey,
			ShortID:               shortID,
			SupportX25519MLKEM768: true,
		}
		params.Set("security", "reality")
		params.Set("pbk", publicKey)
		params.Set("fp", fingerprint)
		if serverName != "" {
			params.Set("sni", serverName)
		}
		if shortID != "" {
			params.Set("sid", shortID)
		}
	case "tls":
		proxy.TLS = true
		proxy.ServerName = stream.TLSSettings.ServerName
		params.Set("security", "tls")
		if proxy.ServerName != "" {
			params.Set("sni", proxy.ServerName)
		}
	default:
		params.Set("security", "none")
	}

	switch network {
	case "xhttp":
		// These values were verified against the installed Mihomo version. Its
		// XHTTP implementation does not inherit Xray's implicit XMUX defaults.
		reuse := clashXHTTPReuseSettings{MaxConnections: "3", HMaxRequestTimes: "600-900", HMaxReusableSecs: "1800-3000"}
		mode := stream.XHTTPSettings.Mode
		if mode == "" || mode == "auto" {
			mode = "stream-up"
		}
		verify := false
		proxy.Port = inbound.ExternalPort
		proxy.TLS = true
		proxy.ServerName = server
		proxy.Flow = ""
		proxy.ClientFingerprint = "chrome"
		proxy.ALPN = []string{"h2"}
		proxy.SkipCertVerify = &verify
		proxy.XHTTPOpts = &clashXHTTPOpts{Path: stream.XHTTPSettings.Path, Host: server, Mode: mode, ReuseSettings: reuse}
		params.Set("security", "tls")
		params.Set("sni", server)
		params.Set("fp", "chrome")
		params.Set("alpn", "h2")
		params.Set("path", stream.XHTTPSettings.Path)
		params.Set("host", server)
		params.Set("mode", mode)
		extra, err := json.Marshal(map[string]interface{}{"xmux": reuse})
		if err != nil {
			return "", proxy, err
		}
		params.Set("extra", string(extra))
	case "ws":
		proxy.WSOpts = &clashWSOpts{Path: stream.WSSettings.Path, Headers: stream.WSSettings.Headers}
		if stream.WSSettings.Path != "" {
			params.Set("path", stream.WSSettings.Path)
		}
		if host := stream.WSSettings.Headers["Host"]; host != "" {
			params.Set("host", host)
		}
	case "http":
		proxy.Network = "h2"
		params.Set("type", "h2")
		proxy.H2Opts = &clashH2Opts{Path: stream.HTTPSettings.Path, Host: stream.HTTPSettings.Host}
		if stream.HTTPSettings.Path != "" {
			params.Set("path", stream.HTTPSettings.Path)
		}
		if host := strings.Join(stream.HTTPSettings.Host, ","); host != "" {
			params.Set("host", host)
		}
	case "grpc":
		proxy.GRPCOpts = &clashGRPCOpts{GRPCServiceName: stream.GRPCSettings.ServiceName}
		if stream.GRPCSettings.ServiceName != "" {
			params.Set("serviceName", stream.GRPCSettings.ServiceName)
		}
	}
	if proxy.Flow != "" {
		params.Set("flow", proxy.Flow)
	}

	profileURL := &url.URL{
		Scheme:   "vless",
		User:     url.User(client.ID),
		Host:     net.JoinHostPort(server, strconv.Itoa(proxy.Port)),
		RawQuery: params.Encode(),
		Fragment: name,
	}
	return profileURL.String(), proxy, nil
}

func deriveRealityPublicKey(privateKey string) (string, error) {
	decoded, err := decodeRealityKey(strings.TrimSpace(privateKey))
	if err != nil {
		return "", common.NewError("Reality 私钥格式无效")
	}
	private, err := ecdh.X25519().NewPrivateKey(decoded)
	if err != nil {
		return "", common.NewError("Reality 私钥无效")
	}
	return base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), nil
}

func decodeRealityKey(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("unsupported encoding")
}

func normalizeProfileHost(value string) (string, error) {
	host := strings.TrimSpace(value)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.ContainsAny(host, " \t\r\n,/\\@") {
		return "", common.NewError("服务器地址无效")
	}
	return host, nil
}

func directProfileRule(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return "IP-CIDR," + ipv4.String() + "/32,DIRECT,no-resolve"
		}
		return "IP-CIDR6," + ip.String() + "/128,DIRECT,no-resolve"
	}
	return "DOMAIN," + host + ",DIRECT"
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeProfileFileName(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' {
			builder.WriteRune(char)
		} else if builder.Len() == 0 || !strings.HasSuffix(builder.String(), "-") {
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return "vless-profile"
	}
	return name
}
