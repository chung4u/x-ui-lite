package service

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FirewallPortStatus describes whether a TCP port is reachable through the
// server's locally managed firewall. Cloud-provider firewalls are outside the
// server and cannot be inspected or changed here.
type FirewallPortStatus struct {
	Port       int    `json:"port"`
	State      string `json:"state"` // open, blocked, or unknown
	Manager    string `json:"manager"`
	CanOpen    bool   `json:"canOpen"`
	Persistent bool   `json:"persistent"`
	Message    string `json:"message"`
}

type FirewallService struct{}

func validateFirewallPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口必须是 1–65535 之间的整数")
	}
	return nil
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runFirewallCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func activeUFW(output string) bool {
	return strings.Contains(strings.ToLower(output), "status: active")
}

func ufwAllowsPort(output string, port int) bool {
	needle := fmt.Sprintf("%d/TCP", port)
	for _, line := range strings.Split(strings.ToUpper(output), "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, "ALLOW") {
			return true
		}
	}
	return false
}

func (s *FirewallService) CheckTCPPort(port int) (*FirewallPortStatus, error) {
	if err := validateFirewallPort(port); err != nil {
		return nil, err
	}

	if commandAvailable("ufw") {
		output, err := runFirewallCommand("ufw", "status")
		if err == nil && activeUFW(output) {
			if ufwAllowsPort(output, port) {
				return &FirewallPortStatus{Port: port, State: "open", Manager: "UFW", Persistent: true,
					Message: fmt.Sprintf("TCP %d 已在 UFW 中放行。", port)}, nil
			}
			return &FirewallPortStatus{Port: port, State: "blocked", Manager: "UFW", CanOpen: true, Persistent: true,
				Message: fmt.Sprintf("TCP %d 尚未在 UFW 中放行。", port)}, nil
		}
	}

	if commandAvailable("firewall-cmd") {
		if _, err := runFirewallCommand("firewall-cmd", "--state"); err == nil {
			_, queryErr := runFirewallCommand("firewall-cmd", "--query-port="+strconv.Itoa(port)+"/tcp")
			if queryErr == nil {
				return &FirewallPortStatus{Port: port, State: "open", Manager: "firewalld", Persistent: true,
					Message: fmt.Sprintf("TCP %d 已在 firewalld 中放行。", port)}, nil
			}
			return &FirewallPortStatus{Port: port, State: "blocked", Manager: "firewalld", CanOpen: true, Persistent: true,
				Message: fmt.Sprintf("TCP %d 尚未在 firewalld 中放行。", port)}, nil
		}
	}

	if commandAvailable("nft") || commandAvailable("iptables") {
		return &FirewallPortStatus{Port: port, State: "unknown", Manager: "系统防火墙", Persistent: false,
			Message: "检测到 nftables 或 iptables；当前规则结构无法安全自动判断，请手动确认该 TCP 端口已放行。"}, nil
	}

	return &FirewallPortStatus{Port: port, State: "open", Manager: "未启用", Persistent: false,
		Message: "未检测到已启用的 UFW 或 firewalld，本机没有发现需要放行的系统策略。"}, nil
}

func (s *FirewallService) OpenTCPPort(port int) (*FirewallPortStatus, error) {
	status, err := s.CheckTCPPort(port)
	if err != nil {
		return nil, err
	}
	if status.State == "open" {
		return status, nil
	}
	if !status.CanOpen {
		return nil, fmt.Errorf("%s", status.Message)
	}

	switch status.Manager {
	case "UFW":
		if output, err := runFirewallCommand("ufw", "allow", strconv.Itoa(port)+"/tcp", "comment", "x-ui inbound"); err != nil {
			return nil, fmt.Errorf("添加 UFW 规则失败: %s", strings.TrimSpace(output))
		}
	case "firewalld":
		portRule := "--add-port=" + strconv.Itoa(port) + "/tcp"
		if output, err := runFirewallCommand("firewall-cmd", portRule); err != nil {
			return nil, fmt.Errorf("添加 firewalld 运行规则失败: %s", strings.TrimSpace(output))
		}
		if output, err := runFirewallCommand("firewall-cmd", "--permanent", portRule); err != nil {
			return nil, fmt.Errorf("添加 firewalld 持久规则失败: %s", strings.TrimSpace(output))
		}
	default:
		return nil, fmt.Errorf("不支持自动添加 %s 规则", status.Manager)
	}

	updated, err := s.CheckTCPPort(port)
	if err != nil {
		return nil, err
	}
	if updated.State != "open" {
		return nil, fmt.Errorf("规则已提交，但未能确认 TCP %d 已放行", port)
	}
	return updated, nil
}
