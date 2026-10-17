package main

import (
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/Clawdlinux/agentgate/internal/ratelimit"
)

func loadRateLimits() (ratelimit.Settings, error) {
	settings := ratelimit.DefaultSettings()
	for _, field := range []struct {
		name   string
		target *float64
	}{
		{"AGENTGATE_RATE_KEY_RPS", &settings.KeyRPS},
		{"AGENTGATE_RATE_TENANT_RPS", &settings.TenantRPS},
	} {
		if value, ok := os.LookupEnv(field.name); ok {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return ratelimit.Settings{}, fmt.Errorf("%s must be a finite non-negative number", field.name)
			}
			*field.target = parsed
		}
	}
	for _, field := range []struct {
		name   string
		target *int
	}{
		{"AGENTGATE_RATE_KEY_BURST", &settings.KeyBurst},
		{"AGENTGATE_RATE_TENANT_BURST", &settings.TenantBurst},
		{"AGENTGATE_TENANT_MAX_INFLIGHT", &settings.TenantMaxInflight},
	} {
		if value, ok := os.LookupEnv(field.name); ok {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return ratelimit.Settings{}, fmt.Errorf("%s must be a non-negative integer", field.name)
			}
			*field.target = parsed
		}
	}
	tenantEnabled := (settings.TenantRPS > 0 && settings.TenantBurst > 0) || settings.TenantMaxInflight > 0
	if tenantEnabled && (settings.KeyRPS == 0 || settings.KeyBurst == 0) {
		return ratelimit.Settings{}, fmt.Errorf(
			"AGENTGATE_RATE_KEY_RPS and AGENTGATE_RATE_KEY_BURST must be positive while tenant admission is enabled",
		)
	}
	return settings, nil
}
