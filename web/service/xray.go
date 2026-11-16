package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/atomic"
	"os"
	"os/exec"
	"sync"
	"time"
	"x-ui/logger"
	"x-ui/xray"
)

var p *xray.Process
var lock sync.Mutex
var isNeedXrayRestart atomic.Bool
var result string
var pendingTraffic []*xray.Traffic
var lastApplyError string

type XrayService struct {
	inboundService InboundService
	settingService SettingService
}

func (s *XrayService) IsXrayRunning() bool {
	return p != nil && p.IsRunning()
}

func (s *XrayService) GetXrayErr() error {
	if p == nil {
		return nil
	}
	return p.GetErr()
}

func (s *XrayService) GetXrayResult() string {
	if result != "" {
		return result
	}
	if s.IsXrayRunning() {
		return ""
	}
	if p == nil {
		return ""
	}
	result = p.GetResult()
	return result
}

func (s *XrayService) GetXrayVersion() string {
	if p == nil {
		return "Unknown"
	}
	return p.GetVersion()
}

func (s *XrayService) GetXrayConfig() (*xray.Config, error) {
	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}

	xrayConfig := &xray.Config{}
	err = json.Unmarshal([]byte(templateConfig), xrayConfig)
	if err != nil {
		return nil, err
	}

	inbounds, err := s.inboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		effective, filterErr := s.inboundService.effectiveInbound(inbound, time.Now())
		if filterErr != nil {
			return nil, filterErr
		}
		if effective == nil {
			continue
		}
		inboundConfig := effective.GenXrayInboundConfig()
		xrayConfig.InboundConfigs = append(xrayConfig.InboundConfigs, *inboundConfig)
	}
	if err = xrayConfig.EnableUserTrafficStats(); err != nil {
		return nil, err
	}
	return xrayConfig, nil
}

// collectTrafficLocked serializes reset-on-read with persistence and core restarts.
// Failed database writes stay pending and are retried before another reset read.
func (s *XrayService) collectTrafficLocked() error {
	if len(pendingTraffic) > 0 {
		if err := s.inboundService.AddTraffic(pendingTraffic); err != nil {
			return err
		}
		pendingTraffic = nil
	}
	if p == nil || !p.IsRunning() {
		return nil
	}
	traffic, err := p.GetTraffic(true)
	if err != nil {
		return err
	}
	if err = s.inboundService.AddTraffic(traffic); err != nil {
		pendingTraffic = traffic
		return err
	}
	return nil
}

func (s *XrayService) CollectTraffic() error {
	lock.Lock()
	defer lock.Unlock()
	if err := s.collectTrafficLocked(); err != nil {
		return err
	}
	needsRestart, err := s.inboundService.SyncMonthlyTrafficPeriod(time.Now())
	if needsRestart {
		s.SetToNeedRestart()
	}
	return err
}

func (s *XrayService) RefreshInboundPolicies() error {
	lock.Lock()
	defer lock.Unlock()
	count, err := s.inboundService.DisableInvalidInbounds()
	if err != nil {
		return err
	}
	if count > 0 {
		s.SetToNeedRestart()
	}
	if p != nil && p.IsRunning() {
		desired, err := s.GetXrayConfig()
		if err != nil {
			return err
		}
		if !p.GetConfig().Equals(desired) {
			s.SetToNeedRestart()
		}
	}
	return nil
}

func validateXrayRuntimeConfig(config *xray.Config) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "x-ui-config-check-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = exec.Command(xray.GetBinaryPath(), "run", "-test", "-config", file.Name()).Run(); err != nil {
		return fmt.Errorf("Xray 配置检查失败；现有服务保持不变")
	}
	return nil
}

func (s *XrayService) RestartXray(isForce bool) (err error) {
	lock.Lock()
	defer lock.Unlock()
	defer func() {
		if err != nil {
			lastApplyError = err.Error()
		} else {
			lastApplyError = ""
		}
	}()
	logger.Debug("restart xray, force:", isForce)
	desired, err := s.GetXrayConfig()
	if err != nil {
		return err
	}
	if p != nil && p.IsRunning() && !isForce && p.GetConfig().Equals(desired) {
		return nil
	}
	if err = validateXrayRuntimeConfig(desired); err != nil {
		return err
	}
	var previous *xray.Config
	if p != nil && p.IsRunning() {
		if err = s.collectTrafficLocked(); err != nil {
			return fmt.Errorf("流量落库失败，已取消重载: %w", err)
		}
		// The final sample may exhaust a group; filter once more before replacing it.
		desired, err = s.GetXrayConfig()
		if err != nil {
			return err
		}
		previous = p.GetConfig()
		if err = p.Stop(); err != nil {
			return err
		}
		for n := 0; n < 20 && p.IsRunning(); n++ {
			time.Sleep(25 * time.Millisecond)
		}
	}
	p = xray.NewProcess(desired)
	result = ""
	err = p.Start()
	if err == nil {
		time.Sleep(150 * time.Millisecond)
		if !p.IsRunning() {
			err = errors.New("Xray 未能启动")
		}
	}
	if err != nil && previous != nil {
		p = xray.NewProcess(previous)
		if restoreErr := p.Start(); restoreErr != nil {
			return fmt.Errorf("Xray 重载及恢复均失败")
		}
		return fmt.Errorf("Xray 重载失败，已恢复上一份运行配置")
	}
	return err
}

func (s *XrayService) StopXray() error {
	lock.Lock()
	defer lock.Unlock()
	if err := s.collectTrafficLocked(); err != nil {
		return fmt.Errorf("停止前流量落库失败: %w", err)
	}
	if p == nil || !p.IsRunning() {
		return nil
	}
	return p.Stop()
}

func (s *XrayService) SetToNeedRestart() {
	isNeedXrayRestart.Store(true)
}

func (s *XrayService) IsNeedRestartAndSetFalse() bool {
	return isNeedXrayRestart.CAS(true, false)
}

type XrayApplyState struct {
	Running bool   `json:"running"`
	Applied bool   `json:"applied"`
	Pending bool   `json:"pending"`
	Error   string `json:"error"`
}

func (s *XrayService) GetApplyState() XrayApplyState {
	lock.Lock()
	defer lock.Unlock()
	state := XrayApplyState{Running: p != nil && p.IsRunning(), Pending: isNeedXrayRestart.Load(), Error: lastApplyError}
	if state.Running {
		desired, err := s.GetXrayConfig()
		if err != nil {
			state.Error = "生成配置失败"
		} else {
			state.Applied = p.GetConfig().Equals(desired)
		}
	}
	return state
}
