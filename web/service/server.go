package service

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"x-ui/logger"
	"x-ui/util/sys"
	"x-ui/xray"
)

type ProcessState string

const (
	Running ProcessState = "running"
	Stop    ProcessState = "stop"
	Error   ProcessState = "error"
)

type Status struct {
	T   time.Time `json:"-"`
	Cpu float64   `json:"cpu"`
	Mem struct {
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
	Date string `json:"date"`
	Used uint64 `json:"used"`
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}

type Release struct {
	TagName string `json:"tag_name"`
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
	routes := defaultRouteInterfaces()
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
	return sent, received, nil
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
	sent, received, err := externalNetworkTraffic()
	if err != nil {
		return MonthlyTraffic{}, err
	}
	now := time.Now()

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

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
		s.trafficState.LastRecv = received
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
	if s.trafficState.DailyDownload == nil {
		s.trafficState.DailyDownload = make(map[string]uint64)
		forcePersist = true
	}
	if trimTrafficDailyMap(s.trafficState.DailyTraffic, localNow, trafficRetentionDays) {
		forcePersist = true
	}
	if trimTrafficDailyMap(s.trafficState.DailyUpload, localNow, trafficRetentionDays) {
		forcePersist = true
	}
	if trimTrafficDailyMap(s.trafficState.DailyDownload, localNow, trafficRetentionDays) {
		forcePersist = true
	}

	if s.trafficLimitGB <= 0 {
		if s.trafficState.Initialized || s.trafficState.UsedBytes != 0 {
			forcePersist = true
		}
		s.trafficState.Initialized = false
		s.trafficState.UsedBytes = 0
		s.trafficState.LastSent = sent
		s.trafficState.LastRecv = received
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
			s.trafficState.LastRecv = received
			forcePersist = true
		} else {
			uploadDelta := trafficCounterDelta(sent, s.trafficState.LastSent)
			downloadDelta := trafficCounterDelta(received, s.trafficState.LastRecv)
			delta := uploadDelta + downloadDelta
			dateKey := localNow.Format("2006-01-02")
			s.trafficState.UsedBytes += delta
			s.trafficState.DailyTraffic[dateKey] += delta
			s.trafficState.DailyUpload[dateKey] += uploadDelta
			s.trafficState.DailyDownload[dateKey] += downloadDelta
			s.trafficState.LastSent = sent
			s.trafficState.LastRecv = received
		}
		dateKey := localNow.Format("2006-01-02")
		if _, ok := s.trafficState.DailyTraffic[dateKey]; !ok {
			s.trafficState.DailyTraffic[dateKey] = 0
			forcePersist = true
		}

		limit := uint64(s.trafficLimitGB) * bytesPerGB
		nextReset := trafficNextReset(periodStart, s.trafficResetDay)
		periodDaily := trafficDailyForPeriod(s.trafficState.DailyTraffic, periodStart, localNow)
		averageDaily, projected, confidence := trafficForecast(s.trafficState.UsedBytes, periodDaily, localNow, nextReset)
		remaining := uint64(0)
		if s.trafficState.UsedBytes < limit {
			remaining = limit - s.trafficState.UsedBytes
		}
		percent := float64(0)
		if limit > 0 {
			percent = float64(s.trafficState.UsedBytes) / float64(limit) * 100
			if percent > 100 {
				percent = 100
			}
		}
		s.trafficSnapshot = MonthlyTraffic{
			Enabled:            true,
			Limit:              limit,
			Used:               s.trafficState.UsedBytes,
			Remaining:          remaining,
			Percent:            percent,
			ResetDay:           s.trafficResetDay,
			PeriodStart:        periodStart,
			NextReset:          nextReset,
			Daily:              trafficDailySeries(s.trafficState.DailyTraffic, s.trafficState.DailyUpload, s.trafficState.DailyDownload, localNow, trafficHistoryDays),
			AverageDaily:       averageDaily,
			ProjectedPeriodEnd: projected,
			ForecastConfidence: confidence,
		}
	}

	if forcePersist || now.Sub(s.trafficLastPersist) >= 30*time.Second {
		if err := s.settingService.SaveMonthlyTrafficState(s.trafficState); err != nil {
			return MonthlyTraffic{}, err
		}
		s.trafficLastPersist = now
	}
	return s.trafficSnapshot, nil
}

func (s *ServerService) GetStatus(lastStatus *Status) *Status {
	now := time.Now()
	status := &Status{
		T: now,
	}

	percents, err := cpu.Percent(0, false)
	if err != nil {
		logger.Warning("get cpu percent failed:", err)
	} else {
		status.Cpu = percents[0]
	}

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

	ioStats, err := net.IOCounters(false)
	if err != nil {
		logger.Warning("get io counters failed:", err)
	} else if len(ioStats) > 0 {
		ioStat := ioStats[0]
		status.NetTraffic.Sent = ioStat.BytesSent
		status.NetTraffic.Recv = ioStat.BytesRecv

		if lastStatus != nil {
			duration := now.Sub(lastStatus.T)
			seconds := float64(duration) / float64(time.Second)
			up := uint64(float64(trafficCounterDelta(status.NetTraffic.Sent, lastStatus.NetTraffic.Sent)) / seconds)
			down := uint64(float64(trafficCounterDelta(status.NetTraffic.Recv, lastStatus.NetTraffic.Recv)) / seconds)
			status.NetIO.Up = up
			status.NetIO.Down = down
		}
	} else {
		logger.Warning("can not find io counters")
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

func (s *ServerService) GetXrayVersions() ([]string, error) {
	url := "https://api.github.com/repos/XTLS/Xray-core/releases"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	buffer := bytes.NewBuffer(make([]byte, 8192))
	buffer.Reset()
	_, err = buffer.ReadFrom(resp.Body)
	if err != nil {
		return nil, err
	}

	releases := make([]Release, 0)
	err = json.Unmarshal(buffer.Bytes(), &releases)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		versions = append(versions, release.TagName)
	}
	return versions, nil
}

func (s *ServerService) downloadXRay(version string) (string, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	switch osName {
	case "darwin":
		osName = "macos"
	}

	switch arch {
	case "amd64":
		arch = "64"
	case "arm64":
		arch = "arm64-v8a"
	}

	fileName := fmt.Sprintf("Xray-%s-%s.zip", osName, arch)
	url := fmt.Sprintf("https://github.com/XTLS/Xray-core/releases/download/%s/%s", version, fileName)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	os.Remove(fileName)
	file, err := os.Create(fileName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", err
	}

	return fileName, nil
}

func (s *ServerService) UpdateXray(version string) error {
	zipFileName, err := s.downloadXRay(version)
	if err != nil {
		return err
	}

	zipFile, err := os.Open(zipFileName)
	if err != nil {
		return err
	}
	defer func() {
		zipFile.Close()
		os.Remove(zipFileName)
	}()

	stat, err := zipFile.Stat()
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(zipFile, stat.Size())
	if err != nil {
		return err
	}

	s.xrayService.StopXray()
	defer func() {
		err := s.xrayService.RestartXray(true)
		if err != nil {
			logger.Error("start xray failed:", err)
		}
	}()

	copyZipFile := func(zipName string, fileName string) error {
		zipFile, err := reader.Open(zipName)
		if err != nil {
			return err
		}
		os.Remove(fileName)
		file, err := os.OpenFile(fileName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fs.ModePerm)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, zipFile)
		return err
	}

	err = copyZipFile("xray", xray.GetBinaryPath())
	if err != nil {
		return err
	}
	err = copyZipFile("geosite.dat", xray.GetGeositePath())
	if err != nil {
		return err
	}
	err = copyZipFile("geoip.dat", xray.GetGeoipPath())
	if err != nil {
		return err
	}

	return nil

}
