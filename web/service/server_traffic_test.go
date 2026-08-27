package service

import (
	"testing"
	"time"
)

func TestTrafficCounterDelta(t *testing.T) {
	tests := []struct {
		name     string
		current  uint64
		previous uint64
		want     uint64
	}{
		{name: "normal increment", current: 150, previous: 100, want: 50},
		{name: "unchanged", current: 100, previous: 100, want: 0},
		{name: "counter reset after reboot", current: 25, previous: 100, want: 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trafficCounterDelta(tt.current, tt.previous); got != tt.want {
				t.Fatalf("trafficCounterDelta(%d, %d) = %d, want %d", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}

func TestTrafficPeriodStart(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	tests := []struct {
		name string
		now  time.Time
		day  int
		want string
	}{
		{
			name: "after reset day uses current month",
			now:  time.Date(2026, time.August, 27, 15, 0, 0, 0, location),
			day:  15,
			want: "2026-08-15",
		},
		{
			name: "before reset day uses previous month",
			now:  time.Date(2026, time.August, 3, 15, 0, 0, 0, location),
			day:  15,
			want: "2026-07-15",
		},
		{
			name: "reset instant starts new period",
			now:  time.Date(2026, time.August, 15, 0, 0, 0, 0, location),
			day:  15,
			want: "2026-08-15",
		},
		{
			name: "day 31 uses previous month last day before current reset",
			now:  time.Date(2026, time.March, 30, 15, 0, 0, 0, location),
			day:  31,
			want: "2026-02-28",
		},
		{
			name: "day 31 resets on shorter month last day",
			now:  time.Date(2026, time.April, 30, 0, 0, 0, 0, location),
			day:  31,
			want: "2026-04-30",
		},
		{
			name: "day 31 uses leap day in February",
			now:  time.Date(2028, time.February, 29, 0, 0, 0, 0, location),
			day:  31,
			want: "2028-02-29",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trafficPeriodStart(tt.now, tt.day).Format("2006-01-02"); got != tt.want {
				t.Fatalf("trafficPeriodStart() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTrafficNextReset(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	tests := []struct {
		start time.Time
		day   int
		want  string
	}{
		{start: time.Date(2026, time.January, 31, 0, 0, 0, 0, location), day: 31, want: "2026-02-28"},
		{start: time.Date(2026, time.February, 28, 0, 0, 0, 0, location), day: 31, want: "2026-03-31"},
		{start: time.Date(2028, time.January, 31, 0, 0, 0, 0, location), day: 31, want: "2028-02-29"},
	}
	for _, tt := range tests {
		if got := trafficNextReset(tt.start, tt.day).Format("2006-01-02"); got != tt.want {
			t.Fatalf("trafficNextReset() = %s, want %s", got, tt.want)
		}
	}
}

func TestIsVirtualTrafficInterface(t *testing.T) {
	for _, name := range []string{"lo", "docker0", "veth123", "br-abcd", "tun0", "tap0"} {
		if !isVirtualTrafficInterface(name) {
			t.Fatalf("%s should be treated as virtual", name)
		}
	}
	for _, name := range []string{"eth0", "ens3", "enp1s0"} {
		if isVirtualTrafficInterface(name) {
			t.Fatalf("%s should be treated as an external interface", name)
		}
	}
}
