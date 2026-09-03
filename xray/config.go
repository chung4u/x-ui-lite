package xray

import (
	"bytes"
	"encoding/json"
	"x-ui/util/json_util"
)

type Config struct {
	LogConfig       json_util.RawMessage `json:"log"`
	RouterConfig    json_util.RawMessage `json:"routing"`
	DNSConfig       json_util.RawMessage `json:"dns"`
	InboundConfigs  []InboundConfig      `json:"inbounds"`
	OutboundConfigs json_util.RawMessage `json:"outbounds"`
	Transport       json_util.RawMessage `json:"transport"`
	Policy          json_util.RawMessage `json:"policy"`
	API             json_util.RawMessage `json:"api"`
	Stats           json_util.RawMessage `json:"stats"`
	Reverse         json_util.RawMessage `json:"reverse"`
	FakeDNS         json_util.RawMessage `json:"fakeDns"`
}

// EnableUserTrafficStats keeps the names shown in the panel unchanged while
// assigning Xray a stable, private identity for every VLESS user statistic.
func (c *Config) EnableUserTrafficStats() error {
	policy := map[string]interface{}{}
	if len(c.Policy) > 0 {
		if err := json.Unmarshal(c.Policy, &policy); err != nil {
			return err
		}
	}
	if policy == nil {
		policy = map[string]interface{}{}
	}
	levels, ok := policy["levels"].(map[string]interface{})
	if !ok {
		levels = map[string]interface{}{}
		policy["levels"] = levels
	}
	level, ok := levels["0"].(map[string]interface{})
	if !ok {
		level = map[string]interface{}{}
		levels["0"] = level
	}
	level["statsUserUplink"] = true
	level["statsUserDownlink"] = true
	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	c.Policy = json_util.RawMessage(encodedPolicy)

	for index := range c.InboundConfigs {
		inbound := &c.InboundConfigs[index]
		if inbound.Protocol != "vless" {
			continue
		}
		settings := map[string]interface{}{}
		if err := json.Unmarshal(inbound.Settings, &settings); err != nil {
			return err
		}
		clients, ok := settings["clients"].([]interface{})
		if !ok {
			continue
		}
		for _, value := range clients {
			client, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			clientID, _ := client["id"].(string)
			if clientID != "" {
				client["email"] = userTrafficEmail(inbound.Tag, clientID)
			}
		}
		encodedSettings, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		inbound.Settings = json_util.RawMessage(encodedSettings)
	}
	return nil
}

func (c *Config) Equals(other *Config) bool {
	if len(c.InboundConfigs) != len(other.InboundConfigs) {
		return false
	}
	for i, inbound := range c.InboundConfigs {
		if !inbound.Equals(&other.InboundConfigs[i]) {
			return false
		}
	}
	if !bytes.Equal(c.LogConfig, other.LogConfig) {
		return false
	}
	if !bytes.Equal(c.RouterConfig, other.RouterConfig) {
		return false
	}
	if !bytes.Equal(c.DNSConfig, other.DNSConfig) {
		return false
	}
	if !bytes.Equal(c.OutboundConfigs, other.OutboundConfigs) {
		return false
	}
	if !bytes.Equal(c.Transport, other.Transport) {
		return false
	}
	if !bytes.Equal(c.Policy, other.Policy) {
		return false
	}
	if !bytes.Equal(c.API, other.API) {
		return false
	}
	if !bytes.Equal(c.Stats, other.Stats) {
		return false
	}
	if !bytes.Equal(c.Reverse, other.Reverse) {
		return false
	}
	if !bytes.Equal(c.FakeDNS, other.FakeDNS) {
		return false
	}
	return true
}
