package service

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"x-ui/database/model"
	"x-ui/util/common"

	"gopkg.in/yaml.v2"
)

type VLESSUserProfile struct {
	Link             string `json:"link"`
	ClashVergeConfig string `json:"clashVergeConfig"`
	FileName         string `json:"fileName"`
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
		ServerNames []string `json:"serverNames"`
		PrivateKey  string   `json:"privateKey"`
		ShortIDs    []string `json:"shortIds"`
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
}

type clashVLESSConfig struct {
	MixedPort   int               `yaml:"mixed-port"`
	Mode        string            `yaml:"mode"`
	LogLevel    string            `yaml:"log-level"`
	Proxies     []clashVLESSProxy `yaml:"proxies"`
	ProxyGroups []clashProxyGroup `yaml:"proxy-groups"`
	Rules       []string          `yaml:"rules"`
}

type clashVLESSProxy struct {
	Name              string            `yaml:"name"`
	Type              string            `yaml:"type"`
	Server            string            `yaml:"server"`
	Port              int               `yaml:"port"`
	UUID              string            `yaml:"uuid"`
	Flow              string            `yaml:"flow,omitempty"`
	Encryption        string            `yaml:"encryption"`
	Network           string            `yaml:"network,omitempty"`
	TLS               bool              `yaml:"tls,omitempty"`
	ServerName        string            `yaml:"servername,omitempty"`
	ClientFingerprint string            `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *clashRealityOpts `yaml:"reality-opts,omitempty"`
	WSOpts            *clashWSOpts      `yaml:"ws-opts,omitempty"`
	H2Opts            *clashH2Opts      `yaml:"h2-opts,omitempty"`
	GRPCOpts          *clashGRPCOpts    `yaml:"grpc-opts,omitempty"`
	UDP               bool              `yaml:"udp"`
}

type clashRealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id,omitempty"`
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
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

func (s *InboundService) GetVLESSUserProfile(inboundID int, userID int, clientID string, host string, requestedName string) (*VLESSUserProfile, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return nil, err
	}
	if inbound.UserId != userID {
		return nil, common.NewError("无权访问该入站")
	}
	if inbound.Protocol != model.VLESS {
		return nil, common.NewError("仅支持 VLESS 入站")
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

	server, err := normalizeProfileHost(host)
	if err != nil {
		return nil, err
	}
	stream := vlessStreamSettings{}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
		return nil, common.NewError("传输配置无效:", err)
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = strings.TrimSpace(client.Email)
	}
	if name == "" {
		name = "用户"
	}
	if strings.TrimSpace(inbound.Remark) != "" {
		name = strings.TrimSpace(inbound.Remark) + " · " + name
	}

	link, proxy, err := buildVLESSProfile(inbound, client, stream, server, name)
	if err != nil {
		return nil, err
	}
	config := clashVLESSConfig{
		MixedPort: 7890,
		Mode:      "rule",
		LogLevel:  "info",
		Proxies:   []clashVLESSProxy{proxy},
		ProxyGroups: []clashProxyGroup{{
			Name:    "PROXY",
			Type:    "select",
			Proxies: []string{name, "DIRECT"},
		}},
		Rules: []string{"MATCH,PROXY"},
	}
	content, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	return &VLESSUserProfile{
		Link:             link,
		ClashVergeConfig: string(content),
		FileName:         safeProfileFileName(name) + ".yaml",
	}, nil
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
		serverName := firstNonEmpty(stream.RealitySettings.ServerNames)
		publicKey, err := deriveRealityPublicKey(stream.RealitySettings.PrivateKey)
		if err != nil {
			return "", proxy, err
		}
		shortID := firstNonEmpty(stream.RealitySettings.ShortIDs)
		proxy.TLS = true
		proxy.ServerName = serverName
		proxy.ClientFingerprint = "chrome"
		proxy.RealityOpts = &clashRealityOpts{PublicKey: publicKey, ShortID: shortID}
		params.Set("security", "reality")
		params.Set("pbk", publicKey)
		params.Set("fp", "chrome")
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
	if client.Flow != "" {
		params.Set("flow", client.Flow)
	}

	profileURL := &url.URL{
		Scheme:   "vless",
		User:     url.User(client.ID),
		Host:     net.JoinHostPort(server, strconv.Itoa(inbound.Port)),
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
	if host == "" || strings.ContainsAny(host, "\r\n/\\") {
		return "", common.NewError("服务器地址无效")
	}
	return host, nil
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
