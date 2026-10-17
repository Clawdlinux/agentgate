package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapacitySkipsPinnedTail(t *testing.T) {
	l := New(nil, WithSettings(Settings{TenantMaxInflight: 1}))
	l.maxEntries = 2
	pinned, delay := l.Acquire("pinned")
	if delay != 0 {
		t.Fatal(delay)
	}
	defer pinned()
	idle, delay := l.Acquire("idle")
	if delay != 0 {
		t.Fatal(delay)
	}
	idle()
	release, delay := l.Acquire("new")
	if delay != 0 {
		t.Fatal("a pinned tail must not hide an evictable entry")
	}
	release()
	if len(l.entries) != 2 {
		t.Fatalf("entries = %d", len(l.entries))
	}
}

func TestCapacitySkipsUndrainedTail(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1.0 / 3600, KeyBurst: 1, TenantMaxInflight: 1}),
		WithClock(func() time.Time { return now }))
	l.maxEntries = 2
	l.Check("slow", "alice", "github")
	idle, delay := l.Acquire("idle")
	if delay != 0 {
		t.Fatal(delay)
	}
	idle()
	release, delay := l.Acquire("new")
	if delay != 0 {
		t.Fatal("an undrained hourly bucket must not hide an evictable entry")
	}
	release()
	if l.Check("slow", "alice", "github") == 0 {
		t.Fatal("the scan must not reset the hourly bucket")
	}
}

func TestCapacityScanIsBounded(t *testing.T) {
	l := New(nil, WithSettings(Settings{TenantMaxInflight: 1}))
	l.maxEntries = 65
	for i := 0; i < 64; i++ {
		release, delay := l.Acquire(fmt.Sprint(i))
		if delay != 0 {
			t.Fatal(delay)
		}
		defer release()
	}
	idle, delay := l.Acquire("idle")
	if delay != 0 {
		t.Fatal(delay)
	}
	idle()
	if _, delay := l.Acquire("new"); delay == 0 {
		t.Fatal("the first scan must stop after 64 pinned entries")
	}
	release, delay := l.Acquire("new")
	if delay != 0 {
		t.Fatal("the scan must rotate pinned entries away from the tail")
	}
	release()
}

func TestPermitCancellationAfterTimeAdvances(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 1}),
		WithClock(func() time.Time { return now }))
	permit, delay := l.CheckKey("a")
	if delay != 0 {
		t.Fatal(delay)
	}
	now = now.Add(500 * time.Millisecond)
	permit.Cancel()
	permit.Cancel()
	permit.Commit()
	next, delay := l.CheckKey("a")
	if delay != 0 {
		t.Fatal("a delayed cancellation must restore the token")
	}
	next.Commit()
	if _, delay := l.CheckKey("a"); delay != time.Second {
		t.Fatalf("cancellation must not exceed burst capacity, retry = %v", delay)
	}
	now = now.Add(time.Second)
	next, delay = l.CheckKey("a")
	if delay != 0 {
		t.Fatal("refill was lost after refund")
	}
	next.Commit()
}

func TestPermitCancellationPreservesOtherCharges(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 2}),
		WithClock(func() time.Time { return now }))
	first, _ := l.CheckKey("a")
	now = now.Add(500 * time.Millisecond)
	second, _ := l.CheckKey("a")
	second.Commit()
	now = now.Add(250 * time.Millisecond)
	first.Cancel()
	third, delay := l.CheckKey("a")
	if delay != 0 {
		t.Fatal(delay)
	}
	third.Commit()
	if _, delay := l.CheckKey("a"); delay != 250*time.Millisecond {
		t.Fatalf("refund changed another charge or refill, retry = %v", delay)
	}
}

func TestConcurrentPermitRefundsPreserveQuota(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 10}),
		WithClock(func() time.Time { return now }))
	var committed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			permit, delay := l.CheckKey("a")
			if delay != 0 {
				return
			}
			if i%2 == 0 {
				permit.Cancel()
			} else {
				permit.Commit()
				committed.Add(1)
			}
		})
	}
	wg.Wait()
	for {
		permit, delay := l.CheckKey("a")
		if delay != 0 {
			break
		}
		permit.Commit()
		committed.Add(1)
		if committed.Load() > 10 {
			t.Fatal("refunds minted extra tokens")
		}
	}
	if committed.Load() != 10 {
		t.Fatalf("committed charges = %d", committed.Load())
	}
	if e := l.entries[bucketKey{scope: keyScope, id: "a"}].Value.(*entry); e.pending != 0 {
		t.Fatalf("pending permits = %d", e.pending)
	}
}

func TestRepeatedLateRefundsPreserveFullBucket(t *testing.T) {
	for _, rps := range []float64{0.4, 1, 17.3} {
		t.Run(fmt.Sprint(rps), func(t *testing.T) {
			now := time.Unix(1000, 0)
			l := New(nil, WithSettings(Settings{KeyRPS: rps, KeyBurst: 1}),
				WithClock(func() time.Time { return now }))
			for i := 0; i < 1000; i++ {
				permit, delay := l.CheckKey("a")
				if delay != 0 {
					t.Fatalf("refunded attempt %d drained the bucket, retry = %v", i, delay)
				}
				now = now.Add(13 * time.Millisecond)
				permit.Cancel()
				now = now.Add(17 * time.Millisecond)
			}
			permit, delay := l.CheckKey("a")
			if delay != 0 {
				t.Fatal(delay)
			}
			permit.Commit()
			if _, delay := l.CheckKey("a"); delay == 0 {
				t.Fatal("refunds exceeded burst capacity")
			}
		})
	}
}
