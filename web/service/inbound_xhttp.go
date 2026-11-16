package service

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
	"x-ui/database/model"
)

var managedHostPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)
var xhttpUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var xhttpPathPattern = regexp.MustCompile(`^/[A-Za-z0-9_./~-]+$`)

type xhttpStream struct {
	Network       string `json:"network"`
	Security      string `json:"security"`
	XHTTPSettings struct {
		Path string `json:"path"`
		Host string `json:"host"`
		Mode string `json:"mode"`
	} `json:"xhttpSettings"`
}

func validateInboundXHTTP(inbound *model.Inbound) error {
	var stream xhttpStream
	if strings.TrimSpace(inbound.StreamSettings) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
		return fmt.Errorf("传输配置格式无效")
	}
	managed := inbound.ExternalHost != "" || inbound.ExternalPort != 0 || inbound.ExternalTLS
	if stream.Network != "xhttp" {
		if managed {
			return validateForwardedRealityEndpoint(inbound)
		}
		return nil
	}
	if inbound.Protocol != model.VLESS {
		return fmt.Errorf("XHTTP 入口须使用 VLESS 认证")
	}
	if !xhttpPathPattern.MatchString(stream.XHTTPSettings.Path) || strings.Contains(stream.XHTTPSettings.Path, "..") || strings.Contains(stream.XHTTPSettings.Path, "//") {
		return fmt.Errorf("XHTTP 路径须以 / 开头且不能包含查询、片段或路径跳转")
	}
	switch stream.XHTTPSettings.Mode {
	case "", "auto", "stream-up", "stream-one", "packet-up":
	default:
		return fmt.Errorf("XHTTP 模式无效")
	}
	settings := vlessInboundSettings{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return fmt.Errorf("XHTTP 用户配置无效")
	}
	var rawSettings struct {
		Clients []map[string]interface{} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(inbound.Settings), &rawSettings); err != nil {
		return fmt.Errorf("XHTTP 用户配置无效")
	}
	for _, client := range rawSettings.Clients {
		for key, value := range client {
			if key != "id" && key != "email" && key != "flow" {
				return fmt.Errorf("XHTTP 用户仅允许 UUID、名称和空 Flow 字段")
			}
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("XHTTP 用户字段须为文本")
			}
			if key == "email" {
				if utf8.RuneCountInString(text) > 128 || strings.IndexFunc(text, unicode.IsControl) >= 0 {
					return fmt.Errorf("用户名称最多 128 个字符且不能含控制字符")
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, client := range settings.Clients {
		if !xhttpUUIDPattern.MatchString(client.ID) || seen[strings.ToLower(client.ID)] {
			return fmt.Errorf("用户 UUID 格式无效或重复")
		}
		seen[strings.ToLower(client.ID)] = true
		if client.Flow != "" {
			return fmt.Errorf("XHTTP 用户 Flow 必须为空")
		}
	}
	if managed {
		if !managedHostPattern.MatchString(inbound.ExternalHost) || strings.Contains(inbound.ExternalHost, "..") || inbound.ExternalPort < 1 || inbound.ExternalPort > 65535 || !inbound.ExternalTLS {
			return fmt.Errorf("外部入口需要有效域名、端口和 TLS")
		}
		ip := net.ParseIP(strings.Trim(inbound.Listen, "[]"))
		if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
			return fmt.Errorf("由 Caddy 接入的 XHTTP 后端必须监听私有或回环地址")
		}
		if stream.Security != "none" && stream.Security != "" {
			return fmt.Errorf("外部 TLS 已由 Caddy 处理，内部 XHTTP 传输应为 none")
		}
	}
	return nil
}

func validateManagedEndpointChange(before, after *model.Inbound) error {
	if before.ExternalHost == "" {
		return nil
	}
	var oldStream, newStream xhttpStream
	if json.Unmarshal([]byte(before.StreamSettings), &oldStream) != nil || json.Unmarshal([]byte(after.StreamSettings), &newStream) != nil {
		return fmt.Errorf("传输配置格式无效")
	}
	var oldDocument, newDocument interface{}
	if json.Unmarshal([]byte(before.StreamSettings), &oldDocument) != nil || json.Unmarshal([]byte(after.StreamSettings), &newDocument) != nil {
		return fmt.Errorf("传输配置格式无效")
	}
	if !reflect.DeepEqual(oldDocument, newDocument) {
		return fmt.Errorf("此入口使用已验证的托管传输配置，修改需通过部署流程执行")
	}
	var oldSettings, newSettings map[string]json.RawMessage
	if json.Unmarshal([]byte(before.Settings), &oldSettings) != nil || json.Unmarshal([]byte(after.Settings), &newSettings) != nil {
		return fmt.Errorf("用户配置格式无效")
	}
	delete(oldSettings, "clients")
	delete(newSettings, "clients")
	normalize := func(settings map[string]json.RawMessage) map[string]interface{} {
		result := map[string]interface{}{}
		for key, value := range settings {
			var decoded interface{}
			if json.Unmarshal(value, &decoded) == nil {
				if key == "fallbacks" && (decoded == nil || reflect.DeepEqual(decoded, []interface{}{})) {
					continue
				}
				result[key] = decoded
			}
		}
		return result
	}
	if !reflect.DeepEqual(normalize(oldSettings), normalize(newSettings)) {
		return fmt.Errorf("托管入口仅允许修改用户，其他认证设置需通过部署流程修改")
	}
	if before.Listen != after.Listen || before.Port != after.Port || before.Protocol != after.Protocol || before.ExternalHost != after.ExternalHost || before.ExternalPort != after.ExternalPort || before.ExternalTLS != after.ExternalTLS || oldStream.Network != newStream.Network || oldStream.Security != newStream.Security || oldStream.XHTTPSettings.Path != newStream.XHTTPSettings.Path || oldStream.XHTTPSettings.Host != newStream.XHTTPSettings.Host {
		return fmt.Errorf("此入口由 Caddy 托管，域名、端口、路径和内部监听需通过部署流程修改")
	}
	return nil
}
