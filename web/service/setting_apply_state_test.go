package service

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"x-ui/web/entity"
)

func TestSettingApplyStateObservesRuntimeRatherThanSavedValues(t *testing.T) {
	defer RecordPanelRuntimeSettings(nil)
	saved := &entity.AllSetting{WebPort: 8080, WebBasePath: "/panel/", WebKeyFile: "/private-key-path", XrayTemplateConfig: "private-template", TimeLocation: "Asia/Shanghai", TrafficLimitGB: 4096, TrafficResetDay: 1}
	RecordPanelRuntimeSettings(nil)
	if CurrentSettingApplyState(saved).Known {
		t.Fatal("unknown runtime must stay unknown")
	}
	RecordPanelRuntimeSettings(saved)
	recordTrafficRuntimeSettings(4096, 1)
	if got := CurrentSettingApplyState(saved); !got.Known || len(got.PendingRestartKeys) != 0 || got.TrafficPending {
		t.Fatalf("unexpected applied state: %+v", got)
	}
	changed := *saved
	changed.WebPort = 9090
	changed.TimeLocation = "UTC"
	changed.TrafficLimitGB = 5000
	for i := 0; i < 2; i++ {
		got := CurrentSettingApplyState(&changed)
		if len(got.PendingRestartKeys) != 2 || got.PendingRestartKeys[0] != "webPort" || got.PendingRestartKeys[1] != "timeLocation" || !got.TrafficPending {
			t.Fatalf("save or reread must not mark restart settings applied: %+v", got)
		}
	}
	recordTrafficRuntimeSettings(5000, 1)
	if got := CurrentSettingApplyState(&changed); got.TrafficPending || len(got.PendingRestartKeys) != 2 {
		t.Fatal("traffic observation must not clear restart changes")
	}
	RecordPanelRuntimeSettings(&changed)
	if got := CurrentSettingApplyState(&changed); len(got.PendingRestartKeys) != 0 || !got.TrafficPending {
		t.Fatal("new startup must reset observation until traffic collector reads settings")
	}
	encoded, _ := json.Marshal(CurrentSettingApplyState(saved))
	if strings.Contains(string(encoded), "private-key-path") || strings.Contains(string(encoded), "private-template") {
		t.Fatal("apply status must not expose config values")
	}
}

func TestSettingApplyStateTrafficOnlyAndConcurrentReads(t *testing.T) {
	defer RecordPanelRuntimeSettings(nil)
	saved := &entity.AllSetting{WebPort: 8080, TrafficLimitGB: 100, TrafficResetDay: 1}
	RecordPanelRuntimeSettings(saved)
	recordTrafficRuntimeSettings(100, 1)
	changed := *saved
	changed.TrafficResetDay = 15
	if got := CurrentSettingApplyState(&changed); len(got.PendingRestartKeys) != 0 || !got.TrafficPending {
		t.Fatal("traffic settings do not require restart")
	}
	if got := CurrentSettingApplyState(saved); len(got.PendingRestartKeys) != 0 || got.TrafficPending {
		t.Fatal("reverting to observed settings clears pending")
	}
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				recordTrafficRuntimeSettings(100, 1)
				CurrentSettingApplyState(saved)
			}
		}()
	}
	wg.Wait()
}

func TestSettingCoreApplyStateUsesActualConfiguration(t *testing.T) {
	state := SettingApplyState{Known: true, PendingRestartKeys: []string{"webPort", "xrayTemplateConfig"}}
	got := WithCoreApplyState(state, XrayApplyState{Running: true, Applied: true})
	if got.CorePending || len(got.PendingRestartKeys) != 1 || got.PendingRestartKeys[0] != "webPort" {
		t.Fatalf("live core observation should clear only the template flag: %+v", got)
	}
	for _, core := range []XrayApplyState{{}, {Running: true}, {Running: true, Applied: true, Error: "rejected"}, {Running: true, Applied: true, Pending: true}} {
		got = WithCoreApplyState(state, core)
		if !got.CorePending || len(got.PendingRestartKeys) != 2 {
			t.Fatal("unconfirmed or failed core must never be marked synchronized")
		}
	}
}
