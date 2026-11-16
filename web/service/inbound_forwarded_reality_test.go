package service

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"x-ui/database/model"

	"gopkg.in/yaml.v2"
)

func forwardedRealityFixture() model.Inbound {
	stream, _ := json.Marshal(map[string]interface{}{
		"network": "tcp", "security": "reality",
		"realitySettings": map[string]interface{}{
			"privateKey":  base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
			"serverNames": []string{"target.example.com"}, "shortIds": []string{"0123456789abcdef"},
		},
	})
	return model.Inbound{Protocol: model.VLESS, Listen: "127.0.0.1", Port: 8443,
		ExternalHost: "node.example.com", ExternalPort: 443,
		Settings:       `{"clients":[{"id":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}]}`,
		StreamSettings: string(stream)}
}

func TestForwardedRealityExportKeepsPublicEndpointAndVision(t *testing.T) {
	inbound := forwardedRealityFixture()
	if err := validateInboundXHTTP(&inbound); err != nil {
		t.Fatal(err)
	}
	profile, err := buildVLESSUserProfile(&inbound, "11111111-1111-1111-1111-111111111111", "untrusted-panel.example", "test")
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(profile.Link)
	if err != nil {
		t.Fatal(err)
	}
	if link.Host != "node.example.com:443" || link.Query().Get("security") != "reality" || link.Query().Get("flow") != "xtls-rprx-vision" || link.Query().Get("sni") != "target.example.com" {
		t.Fatal("URI did not preserve public REALITY settings")
	}
	var config clashVLESSConfig
	if err := yaml.Unmarshal([]byte(profile.ClashVergeConfig), &config); err != nil {
		t.Fatal(err)
	}
	proxy := config.Proxies[0]
	if proxy.Port != 443 || proxy.Server != "node.example.com" || proxy.Network != "tcp" || !proxy.TLS || proxy.Flow != "xtls-rprx-vision" || proxy.RealityOpts == nil || !proxy.RealityOpts.SupportX25519MLKEM768 {
		t.Fatal("YAML did not preserve public REALITY settings")
	}
	proxyJSON, _ := json.Marshal(proxy)
	for _, forbidden := range []string{"127.0.0.1", "8443", "untrusted-panel.example", "xhttp-opts:"} {
		if strings.Contains(profile.Link+string(proxyJSON), forbidden) {
			t.Fatalf("export leaked %s", forbidden)
		}
	}
	if profile.Connection.Port != 443 || profile.Connection.Server != "node.example.com" || profile.Connection.Flow != "xtls-rprx-vision" {
		t.Fatal("connection details differ from export")
	}
}

func TestForwardedRealityRejectsUnsafeEndpoint(t *testing.T) {
	for name, mutate := range map[string]func(*model.Inbound){
		"public bind":     func(i *model.Inbound) { i.Listen = "0.0.0.0" },
		"missing host":    func(i *model.Inbound) { i.ExternalHost = "" },
		"bad host":        func(i *model.Inbound) { i.ExternalHost = "node.example.com/path" },
		"missing port":    func(i *model.Inbound) { i.ExternalPort = 0 },
		"bad port":        func(i *model.Inbound) { i.ExternalPort = 65536 },
		"tls termination": func(i *model.Inbound) { i.ExternalTLS = true },
		"other transport": func(i *model.Inbound) { i.StreamSettings = `{"network":"ws","security":"reality"}` },
	} {
		t.Run(name, func(t *testing.T) {
			i := forwardedRealityFixture()
			mutate(&i)
			if validateInboundXHTTP(&i) == nil {
				t.Fatal("unsafe forwarded endpoint accepted")
			}
		})
	}
}
