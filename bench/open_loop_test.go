//go:build bench

package bench

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

// measuredC64MaxRPS is the highest c64 throughput from the original unprofiled run.
// Keep this fixed so reruns don't silently change their offered load.
const measuredC64MaxRPS = 21614

func arrivalOffset(index int, rate float64) time.Duration {
	return time.Duration(float64(index) * float64(time.Second) / rate)
}

func (s *stack) runOpenLoop(count int, rate float64, samples []sample) (time.Duration, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Every request gets a fixed deadline. Neither completion nor lateness shifts subsequent arrivals.
	epoch := time.Now().Add(10 * time.Millisecond)
	errors := make(chan error, count)
	var done sync.WaitGroup
	for i := range count {
		scheduled := epoch.Add(arrivalOffset(i, rate))
		// Yield instead of sleeping at sub-millisecond intervals. Dispatch lag remains part of the measured latency.
		for time.Now().Before(scheduled) {
			runtime.Gosched()
		}
		done.Add(1)
		go func() {
			defer done.Done()
			body := fmt.Sprintf(`{"service":"mock","action":"echo","on_behalf_of":"bench-user","params":{"worker":%d}}`, i)
			result, err := s.actAt(ctx, i, body, scheduled)
			if err != nil {
				errors <- err
				cancel()
				return
			}
			samples[i] = result
		}()
	}
	done.Wait()
	elapsed := time.Since(epoch)
	close(errors)
	for err := range errors {
		return elapsed, err
	}
	return elapsed, nil
}

func BenchmarkHandleActOpenLoop(b *testing.B) {
	if runtime.GOMAXPROCS(0) != 4 {
		b.Fatal("run with GOMAXPROCS=4 for the Phase 0 comparison")
	}
	for _, delay := range []time.Duration{0, 5 * time.Millisecond} {
		b.Run("delay="+delay.String(), func(b *testing.B) {
			for _, load := range []int{25, 50, 75} {
				rate := float64(measuredC64MaxRPS) * float64(load) / 100
				b.Run(fmt.Sprintf("load=%d/rps=%g", load, rate), func(b *testing.B) {
					// Bound TCP connection bursts, not arrivals. Client-pool queueing remains in scheduled latency.
					s := newStackWithOptions(b, 128, delay, stackOptions{timingSlots: b.N})
					must(b, s.run(warmupRequests, 64, nil))
					samples := make([]sample, b.N)
					b.ResetTimer()
					elapsed, err := s.runOpenLoop(b.N, rate, samples)
					b.StopTimer()
					must(b, err)
					s.checkReceipts(b, warmupRequests+b.N)
					reportSamples(b, samples, elapsed)
					lags := make([]time.Duration, b.N)
					for i, result := range samples {
						lags[i] = result.dispatchLag
					}
					slices.Sort(lags)
					b.ReportMetric(float64(percentile(lags, 99))/float64(time.Microsecond), "p99-dispatch-lag-us")
					b.ReportMetric(rate, "offered-req/s")
				})
			}
		})
	}
}

func BenchmarkHandleActWakeup(b *testing.B) {
	if runtime.GOMAXPROCS(0) != 4 {
		b.Fatal("run with GOMAXPROCS=4 for the Phase 0 comparison")
	}
	for _, mode := range []string{"sleep", "spin"} {
		b.Run(mode, func(b *testing.B) {
			s := newStackWithOptions(b, 1, 5*time.Millisecond, stackOptions{spinUpstream: mode == "spin"})
			must(b, s.run(warmupRequests, 1, nil))
			samples := make([]sample, b.N)
			b.ResetTimer()
			start := time.Now()
			err := s.run(b.N, 1, samples)
			elapsed := time.Since(start)
			b.StopTimer()
			must(b, err)
			s.checkReceipts(b, warmupRequests+b.N)
			reportSamples(b, samples, elapsed)
		})
	}
}

func TestArrivalOffset(t *testing.T) {
	for _, tc := range []struct {
		index int
		rate  float64
		want  time.Duration
	}{
		{0, 5403.5, 0},
		{54035, 5403.5, 10 * time.Second},
		{10807, 10807, time.Second},
		{32421, 16210.5, 2 * time.Second},
	} {
		if got := arrivalOffset(tc.index, tc.rate); got != tc.want {
			t.Errorf("index=%d rate=%g: got %s, want %s", tc.index, tc.rate, got, tc.want)
		}
	}
	// Offsets are computed from the epoch, without accumulating a truncated interval.
	rate := 16210.5
	if got := arrivalOffset(1000000, rate); got != time.Duration(1000000*float64(time.Second)/rate) {
		t.Errorf("accumulated interval rounding: %s", got)
	}
}

func TestScheduledSampleIncludesDispatchDelay(t *testing.T) {
	epoch := time.Unix(0, 0)
	result := timedSample(epoch, epoch.Add(20*time.Millisecond), epoch.Add(27*time.Millisecond), 5*time.Millisecond)
	if result.total != 27*time.Millisecond || result.total-result.upstream != 22*time.Millisecond ||
		result.dispatchLag != 20*time.Millisecond {
		t.Fatalf("scheduled latency omitted delay: %+v", result)
	}
	closed := timedSample(time.Time{}, epoch.Add(20*time.Millisecond), epoch.Add(27*time.Millisecond), 5*time.Millisecond)
	if closed.total != 7*time.Millisecond || closed.dispatchLag != 0 {
		t.Fatalf("closed-loop timing changed: %+v", closed)
	}
}
