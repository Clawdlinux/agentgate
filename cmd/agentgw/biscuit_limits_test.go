/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/delegation"
)

func TestLoadBiscuitLimits(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
		fail  bool
	}{
		{"50ms", 50 * time.Millisecond, false},
		{"250ms", 250 * time.Millisecond, false},
		{"1s", time.Second, false},
		{"1ms", time.Millisecond, false},
		{"5s", 5 * time.Second, false},
		{"999999ns", 0, true},
		{"5.000000001s", 0, true},
		{"1us", 0, true},
		{"0", 0, true},
		{"0s", 0, true},
		{"-1s", 0, true},
		{"invalid", 0, true},
		{"50", 0, true},
		{"", 0, true},
		{"99999999999999999999h", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AGENTGATE_BISCUIT_MAX_DURATION", tc.value)
			got, err := loadBiscuitLimits()
			if tc.fail {
				if err == nil || !strings.Contains(err.Error(), "AGENTGATE_BISCUIT_MAX_DURATION must be a Go duration between 1ms and 5s") {
					t.Fatalf("limits = %+v, error = %v, want a clear duration error", got, err)
				}
				return
			}
			want := delegation.DefaultLimits()
			want.MaxDuration = tc.want
			if err != nil || got != want {
				t.Fatalf("limits = %+v, error = %v, want %+v", got, err, want)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		t.Setenv("AGENTGATE_BISCUIT_MAX_DURATION", "")
		if err := os.Unsetenv("AGENTGATE_BISCUIT_MAX_DURATION"); err != nil {
			t.Fatal(err)
		}
		got, err := loadBiscuitLimits()
		if err != nil || got != delegation.DefaultLimits() {
			t.Fatalf("default limits = %+v, error = %v", got, err)
		}
	})
}

func TestAgentgwRejectsInvalidBiscuitDuration(t *testing.T) {
	const helper = "AGENTGATE_TEST_INVALID_BISCUIT_DURATION"
	if os.Getenv(helper) == "1" {
		main()
		return
	}
	for _, value := range []string{"do-not-echo-this-value", "999999ns", "5.000000001s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(helper, "1")
			t.Setenv("AGENTGATE_BISCUIT_MAX_DURATION", value)
			cmd := exec.Command(os.Args[0], "-test.run=^TestAgentgwRejectsInvalidBiscuitDuration$")
			cmd.Dir = t.TempDir()
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("startup error = %v, output = %s, want exit code 1", err, output)
			}
			if !strings.Contains(string(output), "AGENTGATE_BISCUIT_MAX_DURATION must be a Go duration between 1ms and 5s") {
				t.Fatalf("startup omitted the configuration error: %s", output)
			}
			if strings.Contains(string(output), value) {
				t.Fatalf("startup echoed the configured value: %s", output)
			}
		})
	}
}
