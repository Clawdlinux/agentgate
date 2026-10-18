package main

import (
	"testing"
	"time"
)

func TestPercentileNearestRank(t *testing.T) {
	values := []int64{4e6, 1e6, 3e6, 2e6}
	for _, test := range []struct {
		p    float64
		want float64
	}{{.5, 2}, {.9, 4}, {.99, 4}} {
		if got := percentile(values, test.p); got != test.want {
			t.Fatalf("percentile(%v) = %v, want %v", test.p, got, test.want)
		}
	}
}

func TestWaitUntilPreservesLateSchedule(t *testing.T) {
	scheduled := time.Now().Add(-time.Second)
	waitUntil(scheduled)
	if time.Since(scheduled) < time.Second {
		t.Fatal("late arrivals must retain the original schedule")
	}
}
