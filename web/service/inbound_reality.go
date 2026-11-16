package service

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"x-ui/database/model"
)

// Validate before any database write. An empty identifier is valid, but an
// empty list is not: Xray requires [""] when clients omit their Short ID.
func validateInboundReality(inbound *model.Inbound) error {
	if strings.TrimSpace(inbound.StreamSettings) == "" {
		return nil
	}
	var stream struct {
		Security string `json:"security"`
		Reality  struct {
			Dest        string        `json:"dest"`
			ServerNames []string      `json:"serverNames"`
			PrivateKey  string        `json:"privateKey"`
			ShortIDs    []interface{} `json:"shortIds"`
		} `json:"realitySettings"`
	}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
		return fmt.Errorf("传输配置格式无效: %w", err)
	}
	if stream.Security != "reality" {
		return nil
	}
	if _, err := deriveRealityPublicKey(stream.Reality.PrivateKey); err != nil {
		return fmt.Errorf("REALITY 私钥格式无效，请重新生成独立密钥")
	}
	if len(stream.Reality.ServerNames) == 0 {
		return fmt.Errorf("请填写 REALITY Server Name")
	}
	for _, name := range stream.Reality.ServerNames {
		if !validRealityHostname(name) || net.ParseIP(name) != nil {
			return fmt.Errorf("REALITY Server Name 须为有效域名")
		}
	}
	host, port, err := net.SplitHostPort(stream.Reality.Dest)
	number, portError := strconv.Atoi(port)
	if err != nil || portError != nil || number < 1 || number > 65535 || (!validRealityHostname(host) && net.ParseIP(host) == nil) {
		return fmt.Errorf("REALITY 握手目标须为有效的主机名与端口")
	}
	if len(stream.Reality.ShortIDs) == 0 {
		return fmt.Errorf("REALITY shortIds 不能为空数组；不使用 Short ID 时请提交 [\"\"]")
	}
	for index, value := range stream.Reality.ShortIDs {
		id, ok := value.(string)
		if !ok || len(id) > 16 {
			return fmt.Errorf("REALITY Short ID 第 %d 项须为空字符串或至多 16 位偶数长度十六进制字符", index+1)
		}
		if _, err := hex.DecodeString(id); err != nil {
			return fmt.Errorf("REALITY Short ID 第 %d 项须为空字符串或至多 16 位偶数长度十六进制字符", index+1)
		}
	}
	return nil
}

func validRealityHostname(host string) bool {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char != '-' && !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') {
				return false
			}
		}
	}
	return true
}

// A TCP SNI router forwards the public endpoint without terminating REALITY.
// Keep its public address separate from the private Xray listening socket.
func validateForwardedRealityEndpoint(inbound *model.Inbound) error {
	var stream struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
		return fmt.Errorf("传输配置格式无效")
	}
	ip := net.ParseIP(strings.Trim(inbound.Listen, "[]"))
	if inbound.Protocol != model.VLESS || stream.Network != "tcp" || stream.Security != "reality" || inbound.ExternalTLS ||
		!validRealityHostname(inbound.ExternalHost) || inbound.ExternalPort < 1 || inbound.ExternalPort > 65535 ||
		ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return fmt.Errorf("REALITY 转发入口需要有效公网域名、端口及私有监听，TLS 须由 REALITY 处理")
	}
	return nil
}
