package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionBuckets(t *testing.T) {
	for _, scope := range []string{"key", "tenant", "service"} {
		t.Run(scope, func(t *testing.T) {
			now := time.Unix(1000, 0)
			settings := Settings{}
			configs := map[string]Config{}
			key, tenant, service := "key-1", "alice", "github"
			switch scope {
			case "key":
				settings.KeyRPS, settings.KeyBurst = 0.4, 1
			case "tenant":
				settings.TenantRPS, settings.TenantBurst = 0.4, 1
			case "service":
				configs[service] = Config{RequestsPerSecond: 0.4, Burst: 1}
			}
			l := New(configs, WithSettings(settings), WithClock(func() time.Time { return now }))
			if delay := l.Check(key, tenant, service); delay != 0 {
				t.Fatalf("first delay = %v", delay)
			}
			if scope == "tenant" {
				key = "key-2"
			}
			if delay := l.Check(key, tenant, service); delay != 2500*time.Millisecond {
				t.Fatalf("exhausted delay = %v", delay)
			}
			now = now.Add(500 * time.Millisecond)
			if delay := l.Check(key, tenant, service); delay != 2*time.Second {
				t.Fatalf("rejected reservations must be canceled, delay = %v", delay)
			}
			now = now.Add(2 * time.Second)
			if delay := l.Check(key, tenant, service); delay != 0 {
				t.Fatalf("refilled delay = %v", delay)
			}
		})
	}
}

func TestAdmissionIsolationAndDisabled(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(map[string]Config{"github": {RequestsPerSecond: 1, Burst: 1}},
		WithSettings(Settings{}), WithClock(func() time.Time { return now }))
	for _, pair := range [][2]string{{"a:b", "github"}, {"a", "github"}, {"other", "github"}} {
		if l.Check(pair[0], "alice", pair[1]) != 0 {
			t.Fatal("separate keys must have separate service buckets")
		}
	}
	if l.Check("a", "alice", "GITHUB") == 0 {
		t.Fatal("service case must not bypass the registry limit")
	}
	for i := 0; i < 100; i++ {
		if l.Check("a", "alice", "unconfigured") != 0 {
			t.Fatal("disabled key and tenant buckets must allow requests")
		}
	}
	release, delay := l.Acquire("alice")
	if delay != 0 {
		t.Fatal("zero concurrency cap must disable admission")
	}
	release()
}

func TestAdmissionRollsBackOtherBuckets(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 1, TenantRPS: 1, TenantBurst: 1}),
		WithClock(func() time.Time { return now }))
	l.Check("a", "alice", "github")
	if l.Check("b", "alice", "github") == 0 {
		t.Fatal("tenant should be exhausted")
	}
	if l.Check("b", "bob", "github") != 0 {
		t.Fatal("denial must not spend the unused key token")
	}
}

func TestAdmissionCapacityAndIdleEviction(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(map[string]Config{"github": {RequestsPerSecond: 1, Burst: 1}},
		WithSettings(Settings{KeyRPS: 1, KeyBurst: 1, TenantRPS: 1, TenantBurst: 1}),
		WithClock(func() time.Time { return now }))
	l.maxEntries = 3
	if l.Check("a", "alice", "github") != 0 || len(l.entries) != 3 {
		t.Fatalf("initial entries = %d", len(l.entries))
	}
	for i := 0; i < 100; i++ {
		if l.Check(fmt.Sprint(i), "alice", "github") == 0 {
			t.Fatal("capacity cannot evict a depleted bucket")
		}
		if len(l.entries) > l.maxEntries {
			t.Fatal("entry cap exceeded")
		}
	}
	now = now.Add(11 * time.Minute)
	if l.Check("b", "bob", "github") != 0 {
		t.Fatal("idle full buckets should be evicted")
	}
	if len(l.entries) != 3 {
		t.Fatalf("entries after sweep = %d", len(l.entries))
	}
	if _, ok := l.entries[bucketKey{scope: keyScope, id: "a"}]; ok {
		t.Fatal("idle key survived the sweep")
	}
}

func TestAdmissionDoesNotResetSlowBuckets(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1.0 / 3600, KeyBurst: 1}),
		WithClock(func() time.Time { return now }))
	l.maxEntries = 1
	l.Check("a", "alice", "github")
	now = now.Add(11 * time.Minute)
	if l.Check("b", "bob", "github") == 0 || l.Check("a", "alice", "github") == 0 {
		t.Fatal("idle eviction must not reset an unrefilled bucket")
	}
	now = now.Add(time.Hour)
	if l.Check("b", "bob", "github") != 0 {
		t.Fatal("a full least-recently-used bucket can be evicted")
	}
}

func TestConcurrencyCapacityAndRelease(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{TenantMaxInflight: 1}),
		WithClock(func() time.Time { return now }))
	l.maxEntries = 1
	release, delay := l.Acquire("alice")
	if delay != 0 {
		t.Fatal(delay)
	}
	now = now.Add(time.Hour)
	if _, delay := l.Acquire("bob"); delay == 0 {
		t.Fatal("active semaphore cannot be evicted")
	}
	if _, delay := l.Acquire("alice"); delay == 0 {
		t.Fatal("tenant cap exceeded")
	}
	release()
	release()
	next, delay := l.Acquire("bob")
	if delay != 0 {
		t.Fatal("released slot should permit eviction")
	}
	next()
}

func TestAdmissionConcurrent(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 10, TenantRPS: 1, TenantBurst: 5}),
		WithClock(func() time.Time { return now }))
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			if l.Check("a", "alice", "github") == 0 {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed = %d", allowed.Load())
	}
}

func TestAdmissionDefaultsAndIndependentScopes(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithClock(func() time.Time { return now }))
	for i := 0; i < 40; i++ {
		if l.Check("a", "alice", "github") != 0 {
			t.Fatalf("default burst rejected request %d", i)
		}
	}
	if l.Check("a", "bob", "stripe") == 0 {
		t.Fatal("key limit must span tenants and services")
	}
	if l.Check("b", "alice", "github") != 0 {
		t.Fatal("other keys must retain their own buckets")
	}
	l = New(nil, WithSettings(Settings{TenantRPS: 1, TenantBurst: 1}), WithClock(func() time.Time { return now }))
	if l.Check("a", "alice", "github") != 0 || l.Check("b", "alice", "stripe") == 0 {
		t.Fatal("tenant limit must span keys and services")
	}
	if l.Check("b", "bob", "github") != 0 {
		t.Fatal("other tenants must retain their own buckets")
	}
}

func TestZeroRateOrBurstDisablesBucket(t *testing.T) {
	for _, settings := range []Settings{
		{KeyRPS: 1, TenantRPS: 1},
		{KeyBurst: 1, TenantBurst: 1},
	} {
		l := New(nil, WithSettings(settings))
		for i := 0; i < 100; i++ {
			if l.Check("a", "alice", "github") != 0 {
				t.Fatal("zero rate or burst should disable the bucket")
			}
		}
		if len(l.entries) != 0 {
			t.Fatal("disabled buckets should not allocate entries")
		}
	}
}

func TestConcurrencySeparateTenants(t *testing.T) {
	l := New(nil, WithSettings(Settings{TenantMaxInflight: 1}))
	release, delay := l.Acquire("alice")
	if delay != 0 {
		t.Fatal(delay)
	}
	defer release()
	releaseOther, delay := l.Acquire("bob")
	if delay != 0 {
		t.Fatal("separate tenants must have separate semaphores")
	}
	defer releaseOther()
	if _, delay := l.Acquire("alice"); delay == 0 {
		t.Fatal("first tenant cap should still apply")
	}
}

func TestKeyRejectionDoesNotAllocateTenantState(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(nil, WithSettings(Settings{KeyRPS: 1, KeyBurst: 1, TenantRPS: 1, TenantBurst: 1}),
		WithClock(func() time.Time { return now }))
	l.Check("a", "alice", "github")
	for i := 0; i < 100; i++ {
		if l.Check("a", fmt.Sprint(i), "github") == 0 {
			t.Fatal("key limit should reject each attempt")
		}
	}
	if len(l.entries) != 2 {
		t.Fatalf("key-limited requests allocated tenant state: %d entries", len(l.entries))
	}
}
