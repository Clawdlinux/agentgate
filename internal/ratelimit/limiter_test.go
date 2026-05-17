package ratelimit

import (
	"testing"
)

func TestLimiter_Allow(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"stripe": {RequestsPerSecond: 2, Burst: 2},
	}
	l := New(configs)

	// First 2 should pass (burst).
	if !l.Allow("agent-1", "stripe") {
		t.Fatal("first request should be allowed")
	}
	if !l.Allow("agent-1", "stripe") {
		t.Fatal("second request should be allowed")
	}
	// Third should be rejected (burst exhausted).
	if l.Allow("agent-1", "stripe") {
		t.Fatal("third request should be rate limited")
	}
}

func TestLimiter_NoConfig(t *testing.T) {
	t.Parallel()
	l := New(map[string]Config{})

	// No config = no limit.
	for i := 0; i < 100; i++ {
		if !l.Allow("agent-1", "unknown") {
			t.Fatalf("request %d should be allowed (no config)", i)
		}
	}
}

func TestLimiter_PerAgent(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"stripe": {RequestsPerSecond: 1, Burst: 1},
	}
	l := New(configs)

	// Agent 1 uses its burst.
	l.Allow("agent-1", "stripe")
	if l.Allow("agent-1", "stripe") {
		t.Fatal("agent-1 should be limited")
	}

	// Agent 2 has its own bucket.
	if !l.Allow("agent-2", "stripe") {
		t.Fatal("agent-2 should be allowed (separate bucket)")
	}
}

func TestConfig_Rate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want float64
	}{
		{"per second", Config{RequestsPerSecond: 10}, 10},
		{"per minute", Config{RequestsPerMinute: 60}, 1},
		{"per hour", Config{RequestsPerHour: 3600}, 1},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := float64(tc.cfg.Rate())
			if got != tc.want {
				t.Fatalf("rate = %f, want %f", got, tc.want)
			}
		})
	}
}
