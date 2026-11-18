package service

import (
	"bufio"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"x-ui/logger"
	"x-ui/util/sys"
)

type ProcessState string

const (
	Running ProcessState = "running"
	Stop    ProcessState = "stop"
	Error   ProcessState = "error"
)

type Status struct {
	T                  time.Time `json:"-"`
	networkSampleTime  time.Time
	networkSampleValid bool
	Cpu                float64   `json:"cpu"`
	CPUHealth          CPUHealth `json:"cpuHealth"`
	Mem                struct {
		Current uint64 `json:"current"`
		Total   uint64 `json:"total"`
	} `json:"mem"`
	Swap struct {
		Current uint64 `json:"current"`
		Total   uint64 `json:"total"`
	} `json:"swap"`
	Disk struct {
		Current uint64 `json:"current"`
		Total   uint64 `json:"total"`
	} `json:"disk"`
	Xray struct {
		State    ProcessState `json:"state"`
		ErrorMsg string       `json:"errorMsg"`
		Version  string       `json:"version"`
	} `json:"xray"`
	Uptime   uint64    `json:"uptime"`
	Loads    []float64 `json:"loads"`
	TcpCount int       `json:"tcpCount"`
	UdpCount int       `json:"udpCount"`
	NetIO    struct {
		Up   uint64 `json:"up"`
		Down uint64 `json:"down"`
	} `json:"netIO"`
	NetTraffic struct {
		Sent uint64 `json:"sent"`
		Recv uint64 `json:"recv"`
	} `json:"netTraffic"`
	MonthlyTraffic MonthlyTraffic `json:"monthlyTraffic"`
}

type MonthlyTraffic struct {
	Today              string         `json:"today"`
	Enabled            bool           `json:"enabled"`
	Limit              uint64         `json:"limit"`
	Used               uint64         `json:"used"`
	Remaining          uint64         `json:"remaining"`
	Percent            float64        `json:"percent"`
	ResetDay           int            `json:"resetDay"`
	PeriodStart        time.Time      `json:"periodStart"`
	NextReset          time.Time      `json:"nextReset"`
	Daily              []DailyTraffic `json:"daily"`
	AverageDaily       uint64         `json:"averageDaily"`
	ProjectedPeriodEnd uint64         `json:"projectedPeriodEnd"`
	ForecastConfidence string         `json:"forecastConfidence"`
}

type DailyTraffic struct {
	Estimated bool   `json:"estimated,omitempty"`
	Date      string `json:"date"`
	Used      uint64 `json:"used"`
	Up        uint64 `json:"up"`
	Down      uint64 `json:"down"`
}

type ServerService struct {
	xrayService    XrayService
	settingService SettingService

	trafficMu            sync.Mutex
	trafficState         *MonthlyTrafficState
	trafficLimitGB       int
	trafficResetDay      int
	trafficLocation      *time.Location
	trafficConfigRefresh time.Time
	trafficLastPersist   time.Time
	trafficSnapshot      MonthlyTraffic
}

const bytesPerGB = uint64(1024 * 1024 * 1024)

const (
	trafficHistoryDays   = 14
	trafficRetentionDays = 45
)

func trafficCounterDelta(current, previous uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	// The operating-system counter restarts from zero after a reboot.
	return current
}

func trafficResetDate(year int, month time.Month, resetDay int, location *time.Location) time.Time {
	if resetDay < 1 {
		resetDay = 1
	} else if resetDay > 31 {
		resetDay = 31
	}
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	if resetDay > daysInMonth {
		resetDay = daysInMonth
	}
	return time.Date(year, month, resetDay, 0, 0, 0, 0, location)
}

func trafficPeriodStart(now time.Time, resetDay int) time.Time {
	start := trafficResetDate(now.Year(), now.Month(), resetDay, now.Location())
	if now.Before(start) {
		previousMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
		start = trafficResetDate(previousMonth.Year(), previousMonth.Month(), resetDay, now.Location())
	}
	return start
}

func trafficNextReset(periodStart time.Time, resetDay int) time.Time {
	nextMonth := time.Date(periodStart.Year(), periodStart.Month()+1, 1, 0, 0, 0, 0, periodStart.Location())
	return trafficResetDate(nextMonth.Year(), nextMonth.Month(), resetDay, periodStart.Location())
}

func trafficDailySeries(daily, upload, download map[string]uint64, now time.Time, days int) []DailyTraffic {
	if days < 1 {
		return nil
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	series := make([]DailyTraffic, 0, days)
	for index := 0; index < days; index++ {
		date := day.AddDate(0, 0, index).Format("2006-01-02")
		series = append(series, DailyTraffic{
			Date: date,
			Used: daily[date],
			Up:   upload[date],
			Down: download[date],
		})
	}
	return series
}

func trafficDailyForPeriod(daily map[string]uint64, periodStart, now time.Time) map[string]uint64 {
	result := make(map[string]uint64)
	startKey := periodStart.Format("2006-01-02")
	endKey := now.Format("2006-01-02")
	for date, value := range daily {
		if date >= startKey && date <= endKey {
			result[date] = value
		}
	}
	return result
}

func trafficDailyTotal(daily map[string]uint64) uint64 {
	var total uint64
	for _, value := range daily {
		total += value
	}
	return total
}

func cloneTrafficDaily(daily map[string]uint64) map[string]uint64 {
	cloned := make(map[string]uint64, len(daily))
	for date, value := range daily {
		cloned[date] = value
	}
	return cloned
}

func trimTrafficDailyMap(daily map[string]uint64, now time.Time, retentionDays int) bool {
	if retentionDays < 1 {
		return false
	}
	oldest := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
	changed := false
	for date := range daily {
		if date < oldest {
			delete(daily, date)
			changed = true
		}
	}
	return changed
}

func trafficForecast(used uint64, daily map[string]uint64, now, nextReset time.Time) (average, projected uint64, confidence string) {
	if len(daily) == 0 {
		return 0, used, "insufficient"
	}
	var dailyTotal uint64
	for _, value := range daily {
		dailyTotal += value
	}
	average = dailyTotal / uint64(len(daily))
	remainingDays := math.Max(0, nextReset.Sub(now).Hours()/24)
	projected = used + uint64(math.Round(float64(average)*remainingDays))
	switch {
	case len(daily) >= 7:
		confidence = "high"
	case len(daily) >= 3:
		confidence = "medium"
	default:
		confidence = "low"
	}
	return
}

func defaultRouteInterfaces() map[string]struct{} {
	interfaces := make(map[string]struct{})
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return interfaces
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == "00000000" {
			interfaces[fields[0]] = struct{}{}
		}
	}
	return interfaces
}

func isVirtualTrafficInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, prefix := range []string{"docker", "veth", "br-", "virbr", "tun", "tap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func externalNetworkTraffic() (sent, received uint64, err error) {
	stats, err := net.IOCounters(true)
	if err != nil {
		return 0, 0, err
	}
	sent, received = sumExternalNetworkTraffic(stats, defaultRouteInterfaces())
	return sent, received, nil
}

func sumExternalNetworkTraffic(stats []net.IOCountersStat, routes map[string]struct{}) (sent, received uint64) {
	for _, stat := range stats {
		if len(routes) > 0 {
			if _, ok := routes[stat.Name]; !ok {
				continue
			}
		} else if isVirtualTrafficInterface(stat.Name) {
			continue
		}
		sent += stat.BytesSent
		received += stat.BytesRecv
	}
	return sent, received
}

func (s *ServerService) refreshTrafficConfigLocked(now time.Time) error {
	if s.trafficLocation != nil && now.Sub(s.trafficConfigRefresh) < 10*time.Second {
		return nil
	}
	allSetting, err := s.settingService.GetAllSetting()
	if err != nil {
		return err
	}
	location, err := s.settingService.GetTimeLocation()
	if err != nil {
		return err
	}
	s.trafficLimitGB = allSetting.TrafficLimitGB
	s.trafficResetDay = allSetting.TrafficResetDay
	s.trafficLocation = location
	s.trafficConfigRefresh = now
	return nil
}

func (s *ServerService) RefreshMonthlyTraffic() (MonthlyTraffic, error) {
	return s.refreshMonthlyTraffic(externalNetworkTraffic)
}

func (s *ServerService) refreshMonthlyTraffic(readCounters func() (uint64, uint64, error)) (MonthlyTraffic, error) {
	// Serialize the observation as well as its application. Otherwise a delayed
	// older observation can look like a reboot and add the entire OS counter.
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	sent, _, err := readCounters()
	if err != nil {
		return MonthlyTraffic{}, err
	}
	now := time.Now()

	if err := s.refreshTrafficConfigLocked(now); err != nil {
		return MonthlyTraffic{}, err
	}
	if s.trafficState == nil {
		state, err := s.settingService.GetMonthlyTrafficState()
		if err != nil {
			return MonthlyTraffic{}, err
		}
		s.trafficState = state
	}

	localNow := now.In(s.trafficLocation)
	periodStart := trafficPeriodStart(localNow, s.trafficResetDay)
	periodKey := periodStart.Format("2006-01-02")
	forcePersist := false
	if s.trafficState.PeriodStart != periodKey {
		s.trafficState.Initialized = false
		s.trafficState.PeriodStart = periodKey
		s.trafficState.UsedBytes = 0
		s.trafficState.LastSent = sent
		forcePersist = true
	}
	if s.trafficState.DailyTraffic == nil {
		s.trafficState.DailyTraffic = make(map[string]uint64)
		forcePersist = true
	}
	if s.trafficState.DailyUpload == nil {
		s.trafficState.DailyUpload = make(map[string]uint64)
		forcePersist = true
	}
	if trimTrafficDailyMap(s.trafficState.DailyTraffic, localNow, trafficRetentionDays) {
		forcePersist = true
	}
	if trimTrafficDailyMap(s.trafficState.DailyUpload, localNow, trafficRetentionDays) {
		forcePersist = true
	}

	if s.trafficLimitGB <= 0 {
		if s.trafficState.Initialized || s.trafficState.UsedBytes != 0 {
			forcePersist = true
		}
		s.trafficState.Initialized = false
		s.trafficState.UsedBytes = 0
		s.trafficState.LastSent = sent
		s.trafficSnapshot = MonthlyTraffic{
			Enabled:     false,
			ResetDay:    s.trafficResetDay,
			PeriodStart: periodStart,
			NextReset:   trafficNextReset(periodStart, s.trafficResetDay),
		}
	} else {
		if !s.trafficState.Initialized {
			s.trafficState.Initialized = true
			s.trafficState.LastSent = sent
			forcePersist = true
		} else {
			uploadDelta := trafficCounterDelta(sent, s.trafficState.LastSent)
			dateKey := localNow.Format("2006-01-02")
			s.trafficState.DailyUpload[dateKey] += uploadDelta
			s.trafficState.LastSent = sent
		}
		dateKey := localNow.Format("2006-01-02")
		if _, ok := s.trafficState.DailyUpload[dateKey]; !ok {
			s.trafficState.DailyUpload[dateKey] = 0
			forcePersist = true
		}
		// Cloud providers charge egress traffic. Rebuild the legacy combined map
		// from upload history so upgrades immediately use the same upload-only
		// accounting for the cycle total, forecast and daily trend.
		s.trafficState.DailyTraffic = cloneTrafficDaily(s.trafficState.DailyUpload)

		limit := uint64(s.trafficLimitGB) * bytesPerGB
		nextReset := trafficNextReset(periodStart, s.trafficResetDay)
		periodDaily := trafficDailyForPeriod(s.trafficState.DailyUpload, periodStart, localNow)
		uploadUsed := trafficDailyTotal(periodDaily)
		if s.trafficState.UsedBytes != uploadUsed {
			s.trafficState.UsedBytes = uploadUsed
			forcePersist = true
		}
		averageDaily, projected, confidence := trafficForecast(uploadUsed, periodDaily, localNow, nextReset)
		remaining := uint64(0)
		if uploadUsed < limit {
			remaining = limit - uploadUsed
		}
		percent := float64(0)
		if limit > 0 {
			percent = float64(uploadUsed) / float64(limit) * 100
			if percent > 100 {
				percent = 100
			}
		}
		s.trafficSnapshot = MonthlyTraffic{
			Enabled:            true,
			Limit:              limit,
			Used:               uploadUsed,
			Remaining:          remaining,
			Percent:            percent,
			ResetDay:           s.trafficResetDay,
			PeriodStart:        periodStart,
			NextReset:          nextReset,
			Daily:              trafficDailySeries(s.trafficState.DailyUpload, s.trafficState.DailyUpload, nil, localNow, trafficHistoryDays),
			AverageDaily:       averageDaily,
			ProjectedPeriodEnd: projected,
			ForecastConfidence: confidence,
		}
	}

	// Use the same server time zone as the daily series, independent of the browser.
	s.trafficSnapshot.Today = localNow.Format("2006-01-02")
	for i := range s.trafficSnapshot.Daily {
		s.trafficSnapshot.Daily[i].Estimated = s.trafficState.DailyEstimated[s.trafficSnapshot.Daily[i].Date]
	}

	if forcePersist || now.Sub(s.trafficLastPersist) >= 30*time.Second {
		if err := s.settingService.SaveMonthlyTrafficState(s.trafficState); err != nil {
			return MonthlyTraffic{}, err
		}
		s.trafficLastPersist = now
	}
	recordTrafficRuntimeSettings(s.trafficLimitGB, s.trafficResetDay)
	return s.trafficSnapshot, nil
}

// observeNetworkTraffic shares the public-interface counters used by monthly
// accounting and runtime trends. Invalid observations never become a baseline.
func (status *Status) observeNetworkTraffic(previous *Status, sampledAt time.Time, sent, received uint64, err error) {
	if err != nil {
		return
	}
	status.NetTraffic.Sent, status.NetTraffic.Recv = sent, received
	status.networkSampleTime, status.networkSampleValid = sampledAt, true
	if previous == nil || !previous.networkSampleValid {
		return
	}
	seconds := sampledAt.Sub(previous.networkSampleTime).Seconds()
	if seconds <= 0 {
		return
	}
	status.NetIO.Up = uint64(float64(trafficCounterDelta(sent, previous.NetTraffic.Sent)) / seconds)
	status.NetIO.Down = uint64(float64(trafficCounterDelta(received, previous.NetTraffic.Recv)) / seconds)
}

func (s *ServerService) GetStatus(lastStatus *Status) *Status {
	now := time.Now()
	status := &Status{
		T: now,
	}

	percents, err := cpu.Percent(0, false)
	if err != nil {
		logger.Warning("get cpu percent failed:", err)
	} else if len(percents) > 0 {
		status.Cpu = percents[0]
	}
	runtimeCPUHealth.observe(time.Now(), status.Cpu, err == nil && len(percents) > 0)
	status.CPUHealth = CurrentCPUHealth()

	upTime, err := host.Uptime()
	if err != nil {
		logger.Warning("get uptime failed:", err)
	} else {
		status.Uptime = upTime
	}

	memInfo, err := mem.VirtualMemory()
	if err != nil {
		logger.Warning("get virtual memory failed:", err)
	} else {
		status.Mem.Current = memInfo.Used
		status.Mem.Total = memInfo.Total
	}

	swapInfo, err := mem.SwapMemory()
	if err != nil {
		logger.Warning("get swap memory failed:", err)
	} else {
		status.Swap.Current = swapInfo.Used
		status.Swap.Total = swapInfo.Total
	}

	distInfo, err := disk.Usage("/")
	if err != nil {
		logger.Warning("get dist usage failed:", err)
	} else {
		status.Disk.Current = distInfo.Used
		status.Disk.Total = distInfo.Total
	}

	avgState, err := load.Avg()
	if err != nil {
		logger.Warning("get load avg failed:", err)
	} else {
		status.Loads = []float64{avgState.Load1, avgState.Load5, avgState.Load15}
	}

	sent, received, networkErr := externalNetworkTraffic()
	status.observeNetworkTraffic(lastStatus, time.Now(), sent, received, networkErr)
	if networkErr != nil {
		logger.Warning("get external io counters failed:", networkErr)
	}

	status.TcpCount, err = sys.GetTCPCount()
	if err != nil {
		logger.Warning("get tcp connections failed:", err)
	}

	status.UdpCount, err = sys.GetUDPCount()
	if err != nil {
		logger.Warning("get udp connections failed:", err)
	}

	if s.xrayService.IsXrayRunning() {
		status.Xray.State = Running
		status.Xray.ErrorMsg = ""
	} else {
		err := s.xrayService.GetXrayErr()
		if err != nil {
			status.Xray.State = Error
		} else {
			status.Xray.State = Stop
		}
		status.Xray.ErrorMsg = s.xrayService.GetXrayResult()
	}
	status.Xray.Version = s.xrayService.GetXrayVersion()

	monthlyTraffic, err := s.RefreshMonthlyTraffic()
	if err != nil {
		logger.Warning("refresh monthly traffic failed:", err)
	} else {
		status.MonthlyTraffic = monthlyTraffic
	}

	return status
}
