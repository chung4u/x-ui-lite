package service

import (
	"errors"
	"github.com/shirou/gopsutil/net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"x-ui/database"
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
		map[string]uint64{"2026-08-31": 10, "2026-09-01": 20},
		map[string]uint64{"2026-08-31": 10, "2026-09-01": 20},
		nil,
		now,
		14,
	)
	if len(got) != 14 || got[0].Date != "2026-08-19" || got[13].Date != "2026-09-01" {
		t.Fatalf("trafficDailySeries() returned unexpected range: %#v", got)
	}
	if got[12].Used != 10 || got[12].Up != 10 || got[12].Down != 0 {
		t.Fatalf("trafficDailySeries() should expose upload-only usage: %#v", got[12])
	}
	if got[0].Used != 0 {
		t.Fatalf("trafficDailySeries() should fill missing dates with zero: %#v", got[0])
	}
}

func TestTrafficDailyTotalUsesUploadHistory(t *testing.T) {
	daily := map[string]uint64{"2026-09-01": 100, "2026-09-02": 250}
	if got := trafficDailyTotal(daily); got != 350 {
		t.Fatalf("trafficDailyTotal() = %d, want 350", got)
	}
	cloned := cloneTrafficDaily(daily)
	cloned["2026-09-01"] = 999
	if daily["2026-09-01"] != 100 {
		t.Fatal("cloneTrafficDaily() should not alias the source map")
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

func TestMonthlyTrafficReadsCountersInsideLock(t *testing.T) {
	s := &ServerService{}
	s.trafficMu.Lock()
	observed := make(chan struct{}, 1)
	done := make(chan error, 1)
	readErr := errors.New("observation failed")
	go func() {
		_, err := s.refreshMonthlyTraffic(func() (uint64, uint64, error) {
			observed <- struct{}{}
			return 0, 0, readErr
		})
		done <- err
	}()
	select {
	case <-observed:
		s.trafficMu.Unlock()
		<-done
		t.Fatal("OS counter was observed outside the accounting lock")
	case <-time.After(100 * time.Millisecond):
	}
	s.trafficMu.Unlock()
	if err := <-done; err != readErr {
		t.Fatalf("error = %v", err)
	}
	// Failed observations must release the lock and leave state untouched.
	if s.trafficState != nil {
		t.Fatal("failed read changed state")
	}
}

func TestMonthlyTrafficConcurrentObservations(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "traffic.db")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	date := now.Format("2006-01-02")
	const baseline = uint64(278000000000)
	s := &ServerService{
		trafficLocation: now.Location(), trafficConfigRefresh: now,
		trafficLimitGB: 1024, trafficResetDay: 1,
		trafficState: &MonthlyTrafficState{Initialized: true,
			PeriodStart: trafficPeriodStart(now, 1).Format("2006-01-02"),
			LastSent:    baseline, DailyUpload: map[string]uint64{date: 10},
			DailyEstimated: map[string]bool{date: true}},
	}
	var counter uint64 = baseline
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.refreshMonthlyTraffic(func() (uint64, uint64, error) {
				n := atomic.AddUint64(&counter, 1)
				time.Sleep(time.Millisecond)
				return n, 0, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.trafficState.UsedBytes != 74 {
		t.Fatalf("usage = %d, want 74", s.trafficState.UsedBytes)
	}
	if !s.trafficSnapshot.Daily[len(s.trafficSnapshot.Daily)-1].Estimated {
		t.Fatal("lost recovery marker")
	}
	persisted, err := s.settingService.GetMonthlyTrafficState()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.UsedBytes != 74 || !persisted.DailyEstimated[date] {
		t.Fatalf("unexpected persisted state: %+v", persisted)
	}
}

func TestMonthlyTrafficTodayUsesSeriesTimeZone(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "today.db")); err != nil {
		t.Fatal(err)
	}
	settings := SettingService{}
	for _, zone := range []string{"Pacific/Kiritimati", "America/Adak"} {
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		if err := settings.setString("timeLocation", zone); err != nil {
			t.Fatal(err)
		}
		for _, limit := range []string{"100", "0"} {
			if err := settings.setString("trafficLimitGB", limit); err != nil {
				t.Fatal(err)
			}
			service := ServerService{}
			before := time.Now().In(location).Format("2006-01-02")
			view, err := service.refreshMonthlyTraffic(func() (uint64, uint64, error) { return 100, 200, nil })
			if err != nil {
				t.Fatal(err)
			}
			after := time.Now().In(location).Format("2006-01-02")
			if view.Today != before && view.Today != after {
				t.Fatalf("today is not the server-series date: %s", view.Today)
			}
			if len(view.Daily) > 0 && view.Daily[len(view.Daily)-1].Date != view.Today {
				t.Fatal("today marker must match the sampled daily series")
			}
		}
	}
}

func TestPublicEgressExcludesInternalForwarding(t *testing.T) {
	stats := []net.IOCountersStat{
		{Name: "enp1s0", BytesSent: 2000, BytesRecv: 3000},
		{Name: "lo", BytesSent: 9000, BytesRecv: 9000},
		{Name: "docker0", BytesSent: 7000},
		{Name: "veth123", BytesSent: 7000},
		{Name: "wgcf", BytesSent: 6000},
	}
	sent, recv := sumExternalNetworkTraffic(stats, map[string]struct{}{"enp1s0": {}})
	if sent != 2000 || recv != 3000 {
		t.Fatalf("public counters = %d/%d", sent, recv)
	}
	first := &Status{}
	now := time.Now()
	first.observeNetworkTraffic(nil, now, 1000, 1000, nil)
	second := &Status{}
	second.observeNetworkTraffic(first, now.Add(2*time.Second), sent, recv, nil)
	if second.NetIO.Up != 500 || second.NetIO.Down != 1000 {
		t.Fatalf("wrong speed: %+v", second.NetIO)
	}
}

func TestNetworkObservationInvalidBaseline(t *testing.T) {
	now := time.Now()
	first := &Status{}
	first.observeNetworkTraffic(nil, now, 1000000, 2000000, nil)
	if first.NetIO.Up != 0 {
		t.Fatal("first sample must not produce speed")
	}
	failed := &Status{}
	failed.observeNetworkTraffic(first, now.Add(time.Second), 0, 0, errors.New("read failed"))
	recovered := &Status{}
	recovered.observeNetworkTraffic(failed, now.Add(2*time.Second), 1002000, 2002000, nil)
	if recovered.NetIO.Up != 0 {
		t.Fatal("failure produced a lifetime-counter spike")
	}
	sameTime := &Status{}
	sameTime.observeNetworkTraffic(recovered, now.Add(2*time.Second), 1003000, 2003000, nil)
	if sameTime.NetIO.Up != 0 {
		t.Fatal("zero interval must not produce speed")
	}
	reset := &Status{}
	reset.observeNetworkTraffic(recovered, now.Add(4*time.Second), 200, 400, nil)
	if reset.NetIO.Up != 100 {
		t.Fatalf("reset speed = %d", reset.NetIO.Up)
	}
}
