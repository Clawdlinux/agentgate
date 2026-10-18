package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSummarizeLatencies(t *testing.T) {
	successes := []sample{
		{Total: 10e6, Upstream: 2e6, Dispatch: 1e6},
		{Total: 20e6, Upstream: 4e6, Dispatch: 3e6},
	}
	failures := []sample{{Error: "request failed"}, {Error: "body read failed"}, {Error: "invalid response"}}
	for _, test := range []struct {
		name               string
		samples            []sample
		successful, errors int
		valid              bool
		p50, p99           any
	}{
		{"success", successes, 2, 0, true, float64(8), float64(16)},
		{"partial failure", append(append([]sample{}, failures...), successes...), 2, 3, false, float64(8), nil},
		{"all failed", failures, 0, 3, false, nil, nil},
		{"empty", nil, 0, 0, false, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := summarizeLatencies(test.samples)
			for key, want := range map[string]any{
				"successful": test.successful, "errors": test.errors, "valid": test.valid,
				"p50_ms": test.p50, "p99_ms": test.p99,
			} {
				if got := summary[key]; got != want {
					t.Fatalf("%s=%v, want %v", key, got, want)
				}
			}
			data, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			var encoded map[string]json.RawMessage
			if err := json.Unmarshal(data, &encoded); err != nil {
				t.Fatal(err)
			}
			if !test.valid && string(encoded["p99_ms"]) != "null" {
				t.Fatalf("invalid row must encode p99_ms as null: %s", data)
			}
		})
	}
}

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
