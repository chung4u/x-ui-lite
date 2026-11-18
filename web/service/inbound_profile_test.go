package service

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"x-ui/database"
	"x-ui/database/model"

	"gopkg.in/yaml.v2"
)

func TestGetVLESSUserProfileBuildsRealityClashConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	privateBytes := make([]byte, 32)
	for index := range privateBytes {
		privateBytes[index] = byte(index + 1)
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		t.Fatalf("NewPrivateKey() error = %v", err)
	}
	settings, _ := json.Marshal(vlessInboundSettings{Clients: []vlessInboundClient{{
		ID: "11111111-1111-1111-1111-111111111111", Email: "phone", Flow: "xtls-rprx-vision",
	}}})
	stream := map[string]interface{}{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]interface{}{
			"serverNames":       []string{"www.example.com"},
			"privateKey":        base64.RawURLEncoding.EncodeToString(privateBytes),
			"shortIds":          []string{"abcd"},
			"clientFingerprint": "firefox",
		},
	}
	streamSettings, _ := json.Marshal(stream)
	inbound := &model.Inbound{
		UserId: 1, Port: 443, Tag: "inbound-443", Remark: "主节点", Protocol: model.VLESS, Enable: true,
		Settings: string(settings), StreamSettings: string(streamSettings),
	}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	profile, err := (&InboundService{}).GetVLESSUserProfile(inbound.Id, 1, "11111111-1111-1111-1111-111111111111", "node.example.com", "phone")
	if err != nil {
		t.Fatalf("GetVLESSUserProfile() error = %v", err)
	}
	publicKey := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	for _, want := range []string{
		"vless://11111111-1111-1111-1111-111111111111@node.example.com:443",
		"security=reality",
		"pbk=" + publicKey,
		"reality-opts:",
		"public-key: " + publicKey,
		"short-id: abcd",
		"servername: www.example.com",
		"client-fingerprint: firefox",
		"DOMAIN,node.example.com,DIRECT",
	} {
		if !strings.Contains(profile.Link+"\n"+profile.ClashVergeConfig, want) {
			t.Fatalf("profile missing %q:\n%s\n%s", want, profile.Link, profile.ClashVergeConfig)
		}
	}
	if profile.FileName != "US-phone.yaml" {
		t.Fatalf("FileName = %q", profile.FileName)
	}
	var exported struct {
		Proxies []struct {
			RealityOpts map[string]interface{} `yaml:"reality-opts"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(profile.ClashVergeConfig), &exported); err != nil {
		t.Fatalf("parse Clash config: %v", err)
	}
	if len(exported.Proxies) != 1 || exported.Proxies[0].RealityOpts["support-x25519mlkem768"] != true {
		t.Fatalf("REALITY export must enable the Mihomo hybrid key exchange: %#v", exported.Proxies)
	}
	if got := profile.Connection; got.Name != "US｜phone" || got.Type != "vless" || got.Server != "node.example.com" || got.Port != 443 || got.UUID != "11111111-1111-1111-1111-111111111111" || got.Encryption != "none" || got.Network != "tcp" || !got.UDP || !got.TLS || got.ServerName != "www.example.com" || got.Flow != "xtls-rprx-vision" || got.ClientFingerprint != "firefox" || got.RealityPublicKey != publicKey {
		t.Fatalf("Connection = %#v", got)
	}
}

func TestVLESSProfileUsesOneDisplayNameAcrossDistributionFormats(t *testing.T) {
	const clientID = "11111111-1111-1111-1111-111111111111"
	for _, requestedName := range []string{"", "phone", "another device"} {
		inbound := &model.Inbound{
			Port: 443, Protocol: model.VLESS, Remark: "US | RoyLive",
			Settings:       `{"clients":[{"id":"` + clientID + `","email":"account name"}]}`,
			StreamSettings: `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"www.example.com"}}`,
		}
		profile, err := buildVLESSUserProfile(inbound, clientID, "node.example.com", requestedName)
		if err != nil {
			t.Fatal(err)
		}
		link, err := url.Parse(profile.Link)
		if err != nil {
			t.Fatal(err)
		}
		var config clashVLESSConfig
		if err := yaml.Unmarshal([]byte(profile.ClashVergeConfig), &config); err != nil {
			t.Fatal(err)
		}
		if link.Fragment != "US｜account name" || profile.Connection.Name != link.Fragment || len(config.Proxies) != 1 || config.Proxies[0].Name != link.Fragment || len(config.ProxyGroups) == 0 || config.ProxyGroups[0].Proxies[0] != link.Fragment {
			t.Fatalf("distribution names disagree for requested name %q", requestedName)
		}
		if profile.FileName != "US-account-name.yaml" {
			t.Fatalf("unsafe download filename: %q", profile.FileName)
		}
	}
}

func TestVLESSProfileOmitsRealityOptionsForOtherSecurityModes(t *testing.T) {
	const clientID = "11111111-1111-1111-1111-111111111111"
	for _, security := range []string{"tls", "none"} {
		t.Run(security, func(t *testing.T) {
			inbound := &model.Inbound{
				Port: 443, Protocol: model.VLESS,
				Settings:       `{"clients":[{"id":"` + clientID + `"}]}`,
				StreamSettings: `{"network":"tcp","security":"` + security + `","tlsSettings":{"serverName":"www.example.com"}}`,
			}
			profile, err := buildVLESSUserProfile(inbound, clientID, "node.example.com", "test")
			if err != nil {
				t.Fatalf("buildVLESSUserProfile() error = %v", err)
			}
			if strings.Contains(profile.ClashVergeConfig, "reality-opts") || strings.Contains(profile.ClashVergeConfig, "support-x25519mlkem768") {
				t.Fatalf("%s profile unexpectedly includes REALITY options: %s", security, profile.ClashVergeConfig)
			}
		})
	}
}

func TestGetVLESSUserProfileRejectsOtherUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	inbound := &model.Inbound{UserId: 7, Port: 2443, Tag: "inbound-2443", Protocol: model.VLESS, Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if _, err := (&InboundService{}).GetVLESSUserProfile(inbound.Id, 8, "anything", "example.com", ""); err == nil {
		t.Fatal("GetVLESSUserProfile() should reject another user's inbound")
	}
}

func TestVLESSSubscriptionUsesStableTokenAndExpiresWithRemovedUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-ui.db")
	if err := database.InitDB(path); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	const clientID = "22222222-2222-2222-2222-222222222222"
	inbound := &model.Inbound{UserId: 4, Port: 8443, Tag: "inbound-8443", Protocol: model.VLESS, Enable: true, Settings: `{"clients":[{"id":"` + clientID + `","email":"User 1"}]}`, StreamSettings: `{"network":"tcp","security":"none"}`}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	service := &InboundService{}
	token, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil {
		t.Fatalf("create subscription token: %v", err)
	}
	again, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil || again != token {
		t.Fatalf("subscription token should stay stable: %q, %v", again, err)
	}
	profile, err := service.GetVLESSUserSubscription(token, "node.example.com")
	if err != nil || !strings.Contains(profile.ClashVergeConfig, "server: node.example.com") || !strings.Contains(profile.Link, clientID) {
		t.Fatalf("GetVLESSUserSubscription() = %#v, %v", profile, err)
	}
	inbound.Settings = `{"clients":[]}`
	if err := database.GetDB().Save(inbound).Error; err != nil {
		t.Fatalf("remove client: %v", err)
	}
	if _, err := service.GetVLESSUserSubscription(token, "node.example.com"); err == nil {
		t.Fatal("removed user subscription should be invalid")
	}
}

func managedXHTTPProfileFixture() *model.Inbound {
	return &model.Inbound{
		UserId: 4, Port: 18081, Listen: "172.18.0.1", Tag: "inbound-18081", Protocol: model.VLESS, Enable: true,
		ExternalHost: "node.example.com", ExternalPort: 443, ExternalTLS: true,
		Settings:       `{"clients":[{"id":"33333333-3333-3333-3333-333333333333","email":"phone","flow":""}]}`,
		StreamSettings: `{"network":"xhttp","security":"none","xhttpSettings":{"host":"node.example.com","path":"/connect/test-entry","mode":"auto"}}`,
	}
}

func TestManagedXHTTPProfileUsesPublicTLSAndExplicitReuse(t *testing.T) {
	inbound := managedXHTTPProfileFixture()
	profile, err := buildVLESSUserProfile(inbound, "33333333-3333-3333-3333-333333333333", "untrusted-panel-host.example:8080", "phone")
	if err != nil {
		t.Fatalf("build managed profile: %v", err)
	}
	var config clashVLESSConfig
	if err := yaml.Unmarshal([]byte(profile.ClashVergeConfig), &config); err != nil {
		t.Fatalf("decode YAML: %v", err)
	}
	if len(config.Proxies) != 1 {
		t.Fatalf("proxy count = %d", len(config.Proxies))
	}
	proxy := config.Proxies[0]
	if proxy.Server != "node.example.com" || proxy.Port != 443 || !proxy.TLS || proxy.ServerName != "node.example.com" || proxy.Network != "xhttp" || proxy.Flow != "" || proxy.ClientFingerprint != "chrome" {
		t.Fatalf("public endpoint mismatch: %#v", proxy)
	}
	if proxy.SkipCertVerify == nil || *proxy.SkipCertVerify || !reflect.DeepEqual(proxy.ALPN, []string{"h2"}) {
		t.Fatalf("TLS verification/ALPN mismatch: %#v", proxy)
	}
	if proxy.XHTTPOpts == nil || proxy.XHTTPOpts.Path != "/connect/test-entry" || proxy.XHTTPOpts.Host != "node.example.com" || proxy.XHTTPOpts.Mode != "stream-up" {
		t.Fatalf("XHTTP options = %#v", proxy.XHTTPOpts)
	}
	wantReuse := clashXHTTPReuseSettings{MaxConnections: "3", HMaxRequestTimes: "600-900", HMaxReusableSecs: "1800-3000"}
	if proxy.XHTTPOpts.ReuseSettings != wantReuse {
		t.Fatalf("reuse = %#v", proxy.XHTTPOpts.ReuseSettings)
	}
	for _, forbidden := range []string{"reality-opts", "support-x25519mlkem768", "short-id", "18081", "172.18.0.1", "untrusted-panel-host", "flow:"} {
		if strings.Contains(profile.ClashVergeConfig, forbidden) || strings.Contains(profile.Link, forbidden) {
			t.Fatalf("managed export contains forbidden field %q", forbidden)
		}
	}
	link, err := url.Parse(profile.Link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	q := link.Query()
	if link.Host != "node.example.com:443" || q.Get("type") != "xhttp" || q.Get("security") != "tls" || q.Get("sni") != "node.example.com" || q.Get("host") != "node.example.com" || q.Get("path") != "/connect/test-entry" || q.Get("mode") != "stream-up" || q.Get("alpn") != "h2" || q.Has("flow") {
		t.Fatalf("node URI fields incorrect: %s", profile.Link)
	}
	var extra struct {
		XMUX clashXHTTPReuseSettings `json:"xmux"`
	}
	if err := json.Unmarshal([]byte(q.Get("extra")), &extra); err != nil || extra.XMUX != wantReuse {
		t.Fatalf("URI extra should match YAML reuse: %#v, %v", extra, err)
	}
	got := profile.Connection
	if got.Server != proxy.Server || got.Port != proxy.Port || !got.TLS || got.XHTTPPath != proxy.XHTTPOpts.Path || got.XHTTPMode != proxy.XHTTPOpts.Mode || got.XHTTPHost != proxy.XHTTPOpts.Host || got.SkipCertVerify || got.RealityPublicKey != "" || !reflect.DeepEqual(got.ALPN, proxy.ALPN) {
		t.Fatalf("connection details differ from export: %#v", got)
	}
}

func TestManagedXHTTPProfileRejectsIncompleteOrIncompatibleConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Inbound)
	}{
		{"missing public host", func(i *model.Inbound) { i.ExternalHost = "" }},
		{"missing public port", func(i *model.Inbound) { i.ExternalPort = 0 }},
		{"invalid public port", func(i *model.Inbound) { i.ExternalPort = 65536 }},
		{"public TLS off", func(i *model.Inbound) { i.ExternalTLS = false }},
		{"host injection", func(i *model.Inbound) { i.ExternalHost = "node.example.com\r\nInjected: yes" }},
		{"host includes port", func(i *model.Inbound) { i.ExternalHost = "node.example.com:443" }},
		{"host mismatch", func(i *model.Inbound) { i.ExternalHost = "different.example.com" }},
		{"archived", func(i *model.Inbound) { i.ArchivedAt = 1 }},
		{"Vision", func(i *model.Inbound) {
			i.Settings = strings.ReplaceAll(i.Settings, `"flow":""`, `"flow":"xtls-rprx-vision"`)
		}},
		{"double TLS", func(i *model.Inbound) {
			i.StreamSettings = strings.ReplaceAll(i.StreamSettings, `"security":"none"`, `"security":"tls"`)
		}},
		{"invalid mode", func(i *model.Inbound) {
			i.StreamSettings = strings.ReplaceAll(i.StreamSettings, `"mode":"auto"`, `"mode":"invalid"`)
		}},
		{"missing path", func(i *model.Inbound) {
			i.StreamSettings = strings.ReplaceAll(i.StreamSettings, `"/connect/test-entry"`, `""`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inbound := managedXHTTPProfileFixture()
			test.mutate(inbound)
			if _, err := buildVLESSUserProfile(inbound, "33333333-3333-3333-3333-333333333333", "fallback.example.com", ""); err == nil {
				t.Fatal("invalid managed configuration should fail instead of exporting a fallback")
			}
		})
	}
}

func TestManagedXHTTPProfilePreservesExplicitClientMode(t *testing.T) {
	for _, mode := range []string{"stream-up", "stream-one", "packet-up"} {
		inbound := managedXHTTPProfileFixture()
		inbound.StreamSettings = strings.ReplaceAll(inbound.StreamSettings, `"mode":"auto"`, `"mode":"`+mode+`"`)
		profile, err := buildVLESSUserProfile(inbound, "33333333-3333-3333-3333-333333333333", "", "")
		if err != nil || profile.Connection.XHTTPMode != mode {
			t.Fatalf("mode %q: profile=%#v err=%v", mode, profile, err)
		}
	}
}

func TestVLESSSubscriptionRemappingPreservesTokenAndChangesTransport(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	const clientID = "33333333-3333-3333-3333-333333333333"
	legacy := &model.Inbound{UserId: 4, Port: 5000, Tag: "inbound-5000", Protocol: model.VLESS, Enable: true,
		Settings: `{"clients":[{"id":"` + clientID + `"}]}`, StreamSettings: `{"network":"tcp","security":"none"}`}
	managed := managedXHTTPProfileFixture()
	for _, inbound := range []*model.Inbound{legacy, managed} {
		if err := database.GetDB().Create(inbound).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &InboundService{}
	token, err := service.GetOrCreateVLESSUserSubscriptionToken(legacy.Id, 4, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.InboundSubscription{}).Where("token = ?", token).Update("inbound_id", managed.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(legacy).Updates(map[string]interface{}{"archived_at": 1, "enable": false}).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := service.GetVLESSUserSubscription(token, "panel.example.com:8080")
	if err != nil || profile.Connection.Network != "xhttp" || profile.Connection.Port != 443 || profile.Connection.UUID != clientID {
		t.Fatalf("remapped subscription should use public XHTTP endpoint: %#v, %v", profile, err)
	}
	again, err := service.GetOrCreateVLESSUserSubscriptionToken(managed.Id, 4, clientID)
	if err != nil || again != token {
		t.Fatal("migration must retain the same token")
	}
	for _, update := range []map[string]interface{}{{"enable": false}, {"enable": true, "archived_at": 1}} {
		if err := database.GetDB().Model(managed).Updates(update).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.GetVLESSUserSubscription(token, "panel.example.com"); err == nil {
			t.Fatal("inactive/archived subscription must not expose a usable profile")
		}
	}
}

func TestManagedXHTTPExportsRespectUserAndSharedQuotaPolicies(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	inbound := managedXHTTPProfileFixture()
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatal(err)
	}
	const clientID = "33333333-3333-3333-3333-333333333333"
	group := &model.InboundQuotaGroup{InboundId: inbound.Id, Name: "Shared", Enabled: true, Total: 100}
	if err := database.GetDB().Create(group).Error; err != nil {
		t.Fatal(err)
	}
	policy := &model.InboundUserPolicy{InboundId: inbound.Id, ClientId: clientID, Enabled: true, GroupId: group.Id}
	if err := database.GetDB().Create(policy).Error; err != nil {
		t.Fatal(err)
	}
	service := &InboundService{}
	token, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetVLESSUserProfile(inbound.Id, 4, clientID, "ignored.example", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetVLESSUserProfile(inbound.Id, 5, clientID, "ignored.example", ""); err == nil {
		t.Fatal("profile must enforce owner scope")
	}
	tests := []struct {
		name   string
		group  map[string]interface{}
		policy map[string]interface{}
	}{
		{"disabled user", nil, map[string]interface{}{"enabled": false}},
		{"disabled group", map[string]interface{}{"enabled": false}, nil},
		{"expired group", map[string]interface{}{"expiry_time": int64(1)}, nil},
		{"quota reached", map[string]interface{}{"up": int64(60), "down": int64(40)}, nil},
		{"exhausted group", map[string]interface{}{"exhausted": true}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := database.GetDB().Model(group).Updates(map[string]interface{}{"enabled": true, "expiry_time": 0, "up": 0, "down": 0, "exhausted": false}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.GetDB().Model(policy).Updates(map[string]interface{}{"enabled": true}).Error; err != nil {
				t.Fatal(err)
			}
			if test.group != nil {
				if err := database.GetDB().Model(group).Updates(test.group).Error; err != nil {
					t.Fatal(err)
				}
			}
			if test.policy != nil {
				if err := database.GetDB().Model(policy).Updates(test.policy).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.GetVLESSUserProfile(inbound.Id, 4, clientID, "ignored.example", ""); err == nil {
				t.Fatal("inactive user profile must be rejected")
			}
			if _, err := service.GetVLESSUserSubscription(token, "ignored.example"); err == nil {
				t.Fatal("inactive user subscription must be rejected")
			}
			if _, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID); err == nil {
				t.Fatal("inactive user token distribution must be rejected")
			}
		})
	}
	if err := database.GetDB().Model(group).Updates(map[string]interface{}{"enabled": true, "expiry_time": 0, "up": 0, "down": 0, "exhausted": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(policy).Updates(map[string]interface{}{"enabled": true}).Error; err != nil {
		t.Fatal(err)
	}
	again, err := service.GetOrCreateVLESSUserSubscriptionToken(inbound.Id, 4, clientID)
	if err != nil || again != token {
		t.Fatal("reactivation must retain the subscription token")
	}
}

func TestDeploymentSubscriptionPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("XUI_SUBSCRIPTION_POLICY_FILE", path)
	t.Setenv("XUI_HY2_NAME", "Existing node")
	cfg := clashVLESSConfig{ProxyGroups: []clashProxyGroup{{Name: "PROXY"}, {Name: "optional"}}}
	if err := applySubscriptionPolicy(&cfg); err == nil {
		t.Fatal("unreadable deployment policy ignored")
	}
	if err := os.WriteFile(path, []byte(`{"dns":{"enable":true},"rules":["DOMAIN,example.org,DIRECT","MATCH,PROXY"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applySubscriptionPolicy(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProxyGroups) != 1 || len(cfg.Rules) != 2 || cfg.Rules[0] != "DOMAIN,example.org,DIRECT" || cfg.DNS["enable"] != true || hyProxyName() != "Existing node" {
		t.Fatal("existing deployment policy not preserved")
	}
}
