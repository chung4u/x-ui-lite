package service

import (
	"math"
	"testing"
	"time"
)

func feedHighCPU(tracker *cpuLoadTracker, base time.Time, first, last int) {
	for second := first; second <= last; second += 10 {
		tracker.observe(base.Add(time.Duration(second)*time.Second), 82, true)
	}
}

func TestCPUHighLoadNeeds180SecondsOfConfirmedSamples(t *testing.T) {
	base := time.Unix(1000, 0)
	tracker := &cpuLoadTracker{}
	feedHighCPU(tracker, base, 0, 170)
	tracker.observe(base.Add(179*time.Second), 80, true)
	before := tracker.snapshot(base.Add(179 * time.Second))
	if !before.SampleValid || before.HighForSeconds != 179 || before.SustainedHigh {
		t.Fatalf("179-second spike reported as a problem: %+v", before)
	}
	if tracker.snapshot(base.Add(180 * time.Second)).SustainedHigh {
		t.Fatal("wall-clock time without another sample confirmed high load")
	}
	tracker.observe(base.Add(180*time.Second), 80, true)
	confirmed := tracker.snapshot(base.Add(180 * time.Second))
	if !confirmed.SampleValid || !confirmed.SustainedHigh || confirmed.HighForSeconds != 180 || confirmed.ThresholdPercent != 80 || confirmed.RequiredSeconds != 180 {
		t.Fatalf("180-second sustained load not confirmed: %+v", confirmed)
	}
	tracker.observe(base.Add(181*time.Second), 79.99, true)
	recovered := tracker.snapshot(base.Add(181 * time.Second))
	if !recovered.SampleValid || recovered.SustainedHigh || recovered.HighForSeconds != 0 {
		t.Fatalf("low load did not reset continuity: %+v", recovered)
	}
	feedHighCPU(tracker, base, 182, 352)
	if tracker.snapshot(base.Add(352 * time.Second)).SustainedHigh {
		t.Fatal("new high interval inherited the prior interval")
	}
	tracker.observe(base.Add(362*time.Second), 90, true)
	if !tracker.snapshot(base.Add(362 * time.Second)).SustainedHigh {
		t.Fatal("new complete interval did not confirm")
	}
}

func TestCPUHighLoadMissingInvalidAndStaleSamplesResetContinuity(t *testing.T) {
	base := time.Unix(1000, 0)
	for _, tc := range []struct {
		name    string
		percent float64
		valid   bool
	}{
		{"missing", 82, false}, {"NaN", math.NaN(), true}, {"infinity", math.Inf(1), true}, {"negative", -1, true}, {"over100", 101, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &cpuLoadTracker{}
			feedHighCPU(tracker, base, 0, 180)
			tracker.observe(base.Add(181*time.Second), tc.percent, tc.valid)
			state := tracker.snapshot(base.Add(181 * time.Second))
			if state.SampleValid || state.SustainedHigh || state.HighForSeconds != 0 {
				t.Fatalf("invalid sample retained a confirmed problem: %+v", state)
			}
			tracker.observe(base.Add(190*time.Second), 82, true)
			if tracker.snapshot(base.Add(190*time.Second)).HighForSeconds != 0 {
				t.Fatal("recovery counted the missing interval")
			}
		})
	}
	tracker := &cpuLoadTracker{}
	feedHighCPU(tracker, base, 0, 180)
	if !tracker.snapshot(base.Add(210 * time.Second)).SampleValid {
		t.Fatal("freshness boundary changed")
	}
	stale := tracker.snapshot(base.Add(211 * time.Second))
	if stale.SampleValid || stale.SustainedHigh || stale.HighForSeconds != 0 {
		t.Fatalf("stale observations reported a problem: %+v", stale)
	}
	tracker.observe(base.Add(211*time.Second), 82, true)
	if tracker.snapshot(base.Add(211*time.Second)).HighForSeconds != 0 {
		t.Fatal("large gap counted as continuously high")
	}
	restarted := &cpuLoadTracker{}
	if restarted.snapshot(base.Add(211 * time.Second)).SustainedHigh {
		t.Fatal("fresh process inherited high-load time")
	}
	restarted.observe(base.Add(212*time.Second), 82, true)
	if restarted.snapshot(base.Add(212*time.Second)).HighForSeconds != 0 {
		t.Fatal("first sample inferred prior history")
	}
}

func TestBackgroundCPUCountersTrackWithoutBrowserAndResetFailures(t *testing.T) {
	base := time.Unix(1000, 0)
	tracker := &cpuLoadTracker{}
	collector := &RuntimeCollector{cpuHealth: tracker}
	collector.observeCPU(base, 0, 0)
	if tracker.snapshot(base).SampleValid {
		t.Fatal("counter baseline is not a valid delta sample")
	}
	for second := 10; second <= 180; second += 10 {
		collector.previous = time.Time{} // Unrelated trend windows may be invalid.
		collector.observeCPU(base.Add(time.Duration(second)*time.Second), float64(second)*100, float64(second)*10)
	}
	collector.observeCPU(base.Add(189*time.Second), 18900, 1890)
	if tracker.snapshot(base.Add(189 * time.Second)).SustainedHigh {
		t.Fatal("counter diagnosis confirmed before 180 seconds of valid percentage samples")
	}
	collector.observeCPU(base.Add(190*time.Second), 19000, 1900)
	if !tracker.snapshot(base.Add(190 * time.Second)).SustainedHigh {
		t.Fatal("background samples did not confirm without page polling")
	}
	collector.invalidateCPU(base.Add(191 * time.Second))
	if tracker.snapshot(base.Add(191 * time.Second)).SampleValid {
		t.Fatal("CPU read failure retained a valid diagnosis")
	}
	collector.observeCPU(base.Add(200*time.Second), 20000, 2000)
	if tracker.snapshot(base.Add(200 * time.Second)).SampleValid {
		t.Fatal("first post-failure counter inferred a delta")
	}
	collector.observeCPU(base.Add(210*time.Second), 21000, 2100)
	if tracker.snapshot(base.Add(210*time.Second)).HighForSeconds != 0 {
		t.Fatal("post-failure first delta inherited old duration")
	}
	collector.observeCPU(base.Add(241*time.Second), 24100, 2410)
	if tracker.snapshot(base.Add(241 * time.Second)).SampleValid {
		t.Fatal("large counter-sample gap became a valid interval")
	}
	collector.observeCPU(base.Add(251*time.Second), 100, 10)
	if tracker.snapshot(base.Add(251 * time.Second)).SampleValid {
		t.Fatal("counter restart became a valid high-load interval")
	}
}
