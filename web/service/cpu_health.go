package service

import (
	"math"
	"sync"
	"time"
)

const (
	cpuHighThreshold     = 80.0
	cpuHighRequired      = 180 * time.Second
	cpuObservationMaxGap = 30 * time.Second
)

// CPUHealth describes confirmed observations in this process. It is never
// reconstructed from persisted five-minute averages or browser elapsed time.
type CPUHealth struct {
	SampleValid      bool    `json:"sampleValid"`
	SustainedHigh    bool    `json:"sustainedHigh"`
	HighForSeconds   int64   `json:"highForSeconds"`
	ThresholdPercent float64 `json:"thresholdPercent"`
	RequiredSeconds  int64   `json:"requiredSeconds"`
}

type cpuLoadTracker struct {
	mu        sync.Mutex
	observed  time.Time
	highSince time.Time
	valid     bool
}

var runtimeCPUHealth = &cpuLoadTracker{}

func (tracker *cpuLoadTracker) observe(now time.Time, percent float64, valid bool) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !tracker.observed.IsZero() && now.Before(tracker.observed) {
		return
	}
	valid = valid && !math.IsNaN(percent) && !math.IsInf(percent, 0) && percent >= 0 && percent <= 100
	if !valid || percent < cpuHighThreshold || tracker.observed.IsZero() || now.Sub(tracker.observed) > cpuObservationMaxGap {
		tracker.highSince = time.Time{}
	}
	if valid && percent >= cpuHighThreshold && tracker.highSince.IsZero() {
		tracker.highSince = now
	}
	tracker.observed = now
	tracker.valid = valid
}

func (tracker *cpuLoadTracker) snapshot(now time.Time) CPUHealth {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := CPUHealth{ThresholdPercent: cpuHighThreshold, RequiredSeconds: int64(cpuHighRequired / time.Second)}
	state.SampleValid = tracker.valid && !tracker.observed.IsZero() && !now.Before(tracker.observed) && now.Sub(tracker.observed) <= cpuObservationMaxGap
	if !state.SampleValid || tracker.highSince.IsZero() {
		return state
	}
	// The wall clock alone must never promote an unconfirmed observation.
	duration := tracker.observed.Sub(tracker.highSince)
	state.HighForSeconds = int64(duration / time.Second)
	state.SustainedHigh = duration >= cpuHighRequired
	return state
}

func CurrentCPUHealth() CPUHealth { return runtimeCPUHealth.snapshot(time.Now()) }

func (collector *RuntimeCollector) cpuTracker() *cpuLoadTracker {
	if collector.cpuHealth != nil {
		return collector.cpuHealth
	}
	return runtimeCPUHealth
}

func (collector *RuntimeCollector) invalidateCPU(now time.Time) {
	collector.cpuPrevious = time.Time{}
	collector.cpuTracker().observe(now, 0, false)
}

// Called by the always-running trend collector, independently of foreground
// page polling and of failures in unrelated network/memory trend collection.
func (collector *RuntimeCollector) observeCPU(now time.Time, total, idle float64) {
	tracker := collector.cpuTracker()
	if math.IsNaN(total) || math.IsNaN(idle) || math.IsInf(total, 0) || math.IsInf(idle, 0) || total < 0 || idle < 0 || idle > total {
		collector.cpuPrevious = time.Time{}
		tracker.observe(now, 0, false)
		return
	}
	previous := collector.cpuPrevious
	totalDelta, idleDelta := total-collector.cpuTotal, idle-collector.cpuIdle
	collector.cpuPrevious = now
	collector.cpuTotal = total
	collector.cpuIdle = idle
	if previous.IsZero() {
		return
	}
	if !now.After(previous) || now.Sub(previous) > cpuObservationMaxGap || totalDelta <= 0 || idleDelta < 0 || idleDelta > totalDelta {
		tracker.observe(now, 0, false)
		return
	}
	tracker.observe(now, 100*(1-idleDelta/totalDelta), true)
}
