package xray

import (
	"encoding/json"
	"testing"
	"x-ui/util/json_util"
)

func TestEnableUserTrafficStats(t *testing.T) {
	config := &Config{
		Policy: json_util.RawMessage(`{"system":{"statsInboundUplink":true}}`),
		InboundConfigs: []InboundConfig{{
			Tag:      "inbound-2443",
			Protocol: "vless",
			Settings: json_util.RawMessage(`{"clients":[{"id":"11111111-1111-1111-1111-111111111111","email":"iPhone"}]}`),
		}},
	}
	if err := config.EnableUserTrafficStats(); err != nil {
		t.Fatalf("EnableUserTrafficStats() error = %v", err)
	}

	policy := map[string]interface{}{}
	if err := json.Unmarshal(config.Policy, &policy); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}
	levels := policy["levels"].(map[string]interface{})
	level := levels["0"].(map[string]interface{})
	if level["statsUserUplink"] != true || level["statsUserDownlink"] != true {
		t.Fatalf("user statistics were not enabled: %#v", level)
	}

	settings := map[string]interface{}{}
	if err := json.Unmarshal(config.InboundConfigs[0].Settings, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	client := settings["clients"].([]interface{})[0].(map[string]interface{})
	if client["email"] != userTrafficEmail("inbound-2443", "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("runtime client email = %#v", client["email"])
	}
}
