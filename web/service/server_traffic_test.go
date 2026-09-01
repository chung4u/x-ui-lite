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

func TestTrafficDailySeriesReturnsRollingTwoWeeks(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, location)
	got := trafficDailySeries(
		map[string]uint64{"2026-08-31": 30, "2026-09-01": 50},
		map[string]uint64{"2026-08-31": 10, "2026-09-01": 20},
		map[string]uint64{"2026-08-31": 20, "2026-09-01": 30},
		now,
		14,
	)
	if len(got) != 14 || got[0].Date != "2026-08-19" || got[13].Date != "2026-09-01" {
		t.Fatalf("trafficDailySeries() returned unexpected range: %#v", got)
	}
	if got[12].Used != 30 || got[12].Up != 10 || got[12].Down != 20 {
		t.Fatalf("trafficDailySeries() lost upload/download detail: %#v", got[12])
	}
	if got[0].Used != 0 {
		t.Fatalf("trafficDailySeries() should fill missing dates with zero: %#v", got[0])
	}
}

func TestTrafficDailyForPeriodKeepsForecastInsideCurrentCycle(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	periodStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, location)
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, location)
	got := trafficDailyForPeriod(map[string]uint64{
		"2026-08-31": 100,
		"2026-09-01": 200,
		"2026-09-02": 300,
	}, periodStart, now)
	if len(got) != 2 || got["2026-08-31"] != 0 || got["2026-09-01"] != 200 || got["2026-09-02"] != 300 {
		t.Fatalf("trafficDailyForPeriod() = %#v", got)
	}
}

func TestTrimTrafficDailyMap(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, location)
	daily := map[string]uint64{"2026-07-01": 1, "2026-08-19": 2, "2026-09-01": 3}
	if !trimTrafficDailyMap(daily, now, 14) {
		t.Fatal("trimTrafficDailyMap() should report removed history")
	}
	if len(daily) != 2 || daily["2026-08-19"] != 2 || daily["2026-09-01"] != 3 {
		t.Fatalf("trimTrafficDailyMap() = %#v", daily)
	}
}

func TestTrafficForecast(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.August, 28, 0, 0, 0, 0, location)
	nextReset := time.Date(2026, time.August, 31, 0, 0, 0, 0, location)
	average, projected, confidence := trafficForecast(300, map[string]uint64{
		"2026-08-26": 90,
		"2026-08-27": 110,
	}, now, nextReset)
	if average != 100 || projected != 600 || confidence != "low" {
		t.Fatalf("trafficForecast() = (%d, %d, %q), want (100, 600, low)", average, projected, confidence)
	}

	average, projected, confidence = trafficForecast(300, nil, now, nextReset)
	if average != 0 || projected != 300 || confidence != "insufficient" {
		t.Fatalf("trafficForecast() without history = (%d, %d, %q)", average, projected, confidence)
	}
}
