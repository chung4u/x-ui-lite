package service

import (
	"fmt"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"sync"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/sys"
)

type RuntimeCollector struct {
	mu                sync.Mutex
	previous          time.Time
	started           time.Time
	total, idle       float64
	sent              uint64
	n                 int
	cpuSum, upSum     float64
	row               model.RuntimeSample
	cpuPrevious       time.Time
	cpuTotal, cpuIdle float64
	cpuHealth         *cpuLoadTracker
}

const (
	runtimeTrendWindow      = 14 * 24 * time.Hour
	runtimeTrendSampleLimit = 14 * 24 * 12
)

func RuntimeTrend() ([]model.RuntimeSample, error) {
	rows := make([]model.RuntimeSample, 0)
	err := database.GetDB().Where("time > ?", time.Now().Add(-runtimeTrendWindow).Unix()).Order("time ASC").Limit(runtimeTrendSampleLimit).Find(&rows).Error
	return rows, err
}

// Collect observes ten-second deltas; only complete five-minute windows are persisted.
func (r *RuntimeCollector) Collect() (collectErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		// A failed observation invalidates this window; never present missing data as healthy.
		if collectErr != nil {
			r.previous = time.Time{}
		}
	}()
	now := time.Now()
	times, err := cpu.Times(false)
	if err != nil {
		r.invalidateCPU(now)
		return err
	}
	if len(times) == 0 {
		r.invalidateCPU(now)
		return fmt.Errorf("CPU counters unavailable")
	}
	c := times[0]
	total := c.User + c.System + c.Idle + c.Nice + c.Iowait + c.Irq + c.Softirq + c.Steal
	idle := c.Idle + c.Iowait
	r.observeCPU(now, total, idle)
	sent, _, err := externalNetworkTraffic()
	if err != nil {
		return err
	}
	defer func() { r.previous = now; r.total = total; r.idle = idle; r.sent = sent }()
	if r.previous.IsZero() || now.Sub(r.previous) > 30*time.Second {
		r.started = now
		r.n = 0
		r.cpuSum = 0
		r.upSum = 0
		r.row = model.RuntimeSample{}
		return database.GetDB().Where("time <= ?", now.Add(-runtimeTrendWindow).Unix()).Delete(&model.RuntimeSample{}).Error
	}
	m, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	tcp, err := sys.GetTCPCount()
	if err != nil {
		return err
	}
	udp, err := sys.GetUDPCount()
	if err != nil {
		return err
	}
	usage := 0.0
	if total > r.total {
		usage = 100 * (1 - (idle-r.idle)/(total-r.total))
	}
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	up := float64(trafficCounterDelta(sent, r.sent)) / now.Sub(r.previous).Seconds()
	r.n++
	r.cpuSum += usage
	r.upSum += up
	if usage > r.row.CPUPeak {
		r.row.CPUPeak = usage
	}
	if up > r.row.UploadPeak {
		r.row.UploadPeak = up
	}
	r.row.Memory = m.UsedPercent
	r.row.Connections = tcp + udp
	if now.Sub(r.started) < 5*time.Minute {
		return nil
	}
	r.row.Time = now.Unix()
	r.row.CPU = r.cpuSum / float64(r.n)
	r.row.Upload = r.upSum / float64(r.n)
	x := &XrayService{}
	r.row.State = "stop"
	if x.IsXrayRunning() {
		r.row.State = "running"
	} else if x.GetXrayErr() != nil {
		r.row.State = "error"
	}
	if err := database.GetDB().Create(&r.row).Error; err != nil {
		return err
	}
	r.started = now
	r.n = 0
	r.cpuSum = 0
	r.upSum = 0
	r.row = model.RuntimeSample{}
	return database.GetDB().Where("time <= ?", now.Add(-runtimeTrendWindow).Unix()).Delete(&model.RuntimeSample{}).Error
}
