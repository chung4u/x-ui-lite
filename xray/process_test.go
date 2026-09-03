package xray

import "testing"

func TestParseTrafficName(t *testing.T) {
	tests := []struct {
		name     string
		statName string
		inbound  bool
		tag      string
		down     bool
		matched  bool
	}{
		{
			name:     "inbound downlink",
			statName: "inbound>>>vmess-5000>>>traffic>>>downlink",
			inbound:  true,
			tag:      "vmess-5000",
			down:     true,
			matched:  true,
		},
		{
			name:     "outbound uplink",
			statName: "outbound>>>direct>>>traffic>>>uplink",
			tag:      "direct",
			matched:  true,
		},
		{
			name:     "unrelated xray statistic",
			statName: "user>>>example@example.com>>>traffic>>>uplink",
		},
		{
			name: "empty statistic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, tag, down, matched := parseTrafficName(tt.statName)
			if inbound != tt.inbound || tag != tt.tag || down != tt.down || matched != tt.matched {
				t.Fatalf("parseTrafficName(%q) = (%v, %q, %v, %v)", tt.statName, inbound, tag, down, matched)
			}
		})
	}
}

func TestParseUserTrafficName(t *testing.T) {
	name := "user>>>" + userTrafficEmail("inbound-2443", "11111111-1111-1111-1111-111111111111") + ">>>traffic>>>uplink"
	tag, clientID, down, ok := parseUserTrafficName(name)
	if !ok || tag != "inbound-2443" || clientID != "11111111-1111-1111-1111-111111111111" || down {
		t.Fatalf("parseUserTrafficName(%q) = (%q, %q, %v, %v)", name, tag, clientID, down, ok)
	}
}
