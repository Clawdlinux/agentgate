package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/ratelimit"
)

var rateEnvNames = []string{
	"AGENTGATE_RATE_KEY_RPS", "AGENTGATE_RATE_KEY_BURST",
	"AGENTGATE_RATE_TENANT_RPS", "AGENTGATE_RATE_TENANT_BURST",
	"AGENTGATE_TENANT_MAX_INFLIGHT",
}

func clearRateEnv(t *testing.T) {
	t.Helper()
	for _, name := range rateEnvNames {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadRateLimits(t *testing.T) {
	clearRateEnv(t)
	settings, err := loadRateLimits()
	if err != nil || settings != ratelimit.DefaultSettings() {
		t.Fatalf("defaults = %+v, err = %v", settings, err)
	}
	for _, name := range rateEnvNames {
		t.Setenv(name, "0")
	}
	settings, err = loadRateLimits()
	if err != nil || settings != (ratelimit.Settings{}) {
		t.Fatalf("disabled = %+v, err = %v", settings, err)
	}
	t.Setenv("AGENTGATE_RATE_KEY_RPS", "0.5")
	t.Setenv("AGENTGATE_RATE_KEY_BURST", "2")
	t.Setenv("AGENTGATE_RATE_TENANT_RPS", "12.5")
	t.Setenv("AGENTGATE_RATE_TENANT_BURST", "25")
	t.Setenv("AGENTGATE_TENANT_MAX_INFLIGHT", "7")
	settings, err = loadRateLimits()
	want := ratelimit.Settings{KeyRPS: 0.5, KeyBurst: 2, TenantRPS: 12.5, TenantBurst: 25, TenantMaxInflight: 7}
	if err != nil || settings != want {
		t.Fatalf("overrides = %+v, err = %v", settings, err)
	}
}

func TestLoadRateLimitsInvalid(t *testing.T) {
	for _, name := range rateEnvNames {
		for _, value := range []string{"", "-1", "NaN", "Inf", "1e999", "invalid"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				clearRateEnv(t)
				t.Setenv(name, value)
				_, err := loadRateLimits()
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("expected named validation error, got %v", err)
				}
			})
		}
		if strings.Contains(name, "RPS") {
			continue
		}
		t.Run(name+"/fraction", func(t *testing.T) {
			clearRateEnv(t)
			t.Setenv(name, "1.5")
			if _, err := loadRateLimits(); err == nil {
				t.Fatal("fractional integer accepted")
			}
		})
	}
}
