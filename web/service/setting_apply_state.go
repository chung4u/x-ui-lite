package service

import (
	"sync"
	"x-ui/web/entity"
)

// Runtime values are process-local observations, never persisted settings.
// A saved configuration cannot by itself prove that the running listener uses it.
var panelSettingRuntime struct {
	sync.RWMutex
	startup      *entity.AllSetting
	trafficKnown bool
	trafficLimit int
	trafficReset int
}

type SettingApplyState struct {
	Known              bool     `json:"known"`
	PendingRestartKeys []string `json:"pendingRestartKeys"`
	TrafficPending     bool     `json:"trafficPending"`
	CorePending        bool     `json:"corePending"`
}

func WithCoreApplyState(state SettingApplyState, core XrayApplyState) SettingApplyState {
	state.CorePending = !core.Running || !core.Applied || core.Pending || core.Error != ""
	if !state.CorePending {
		keys := make([]string, 0, len(state.PendingRestartKeys))
		for _, key := range state.PendingRestartKeys {
			if key != "xrayTemplateConfig" {
				keys = append(keys, key)
			}
		}
		state.PendingRestartKeys = keys
	}
	return state
}

func RecordPanelRuntimeSettings(setting *entity.AllSetting) {
	panelSettingRuntime.Lock()
	defer panelSettingRuntime.Unlock()
	panelSettingRuntime.startup = nil
	panelSettingRuntime.trafficKnown = false
	if setting != nil {
		copy := *setting
		panelSettingRuntime.startup = &copy
	}
}

func recordTrafficRuntimeSettings(limit, reset int) {
	panelSettingRuntime.Lock()
	defer panelSettingRuntime.Unlock()
	panelSettingRuntime.trafficKnown = true
	panelSettingRuntime.trafficLimit = limit
	panelSettingRuntime.trafficReset = reset
}

func CurrentSettingApplyState(saved *entity.AllSetting) SettingApplyState {
	panelSettingRuntime.RLock()
	defer panelSettingRuntime.RUnlock()
	state := SettingApplyState{PendingRestartKeys: []string{}}
	if saved == nil || panelSettingRuntime.startup == nil {
		return state
	}
	state.Known = true
	live := panelSettingRuntime.startup
	fields := []struct {
		key           string
		current, next interface{}
	}{
		{"webListen", live.WebListen, saved.WebListen},
		{"webPort", live.WebPort, saved.WebPort},
		{"webCertFile", live.WebCertFile, saved.WebCertFile},
		{"webKeyFile", live.WebKeyFile, saved.WebKeyFile},
		{"webBasePath", live.WebBasePath, saved.WebBasePath},
		{"xrayTemplateConfig", live.XrayTemplateConfig, saved.XrayTemplateConfig},
		{"timeLocation", live.TimeLocation, saved.TimeLocation},
	}
	for _, field := range fields {
		if field.current != field.next {
			state.PendingRestartKeys = append(state.PendingRestartKeys, field.key)
		}
	}
	state.TrafficPending = !panelSettingRuntime.trafficKnown || panelSettingRuntime.trafficLimit != saved.TrafficLimitGB || panelSettingRuntime.trafficReset != saved.TrafficResetDay
	return state
}
