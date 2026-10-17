package main

import (
	"strings"
	"testing"
)

func TestTenantAdmissionRequiresKeyLimit(t *testing.T) {
	for _, name := range []string{"AGENTGATE_RATE_KEY_RPS", "AGENTGATE_RATE_KEY_BURST"} {
		for _, enabled := range []string{"rate", "inflight", "neither"} {
			t.Run(name+"/"+enabled, func(t *testing.T) {
				clearRateEnv(t)
				t.Setenv(name, "0")
				t.Setenv("AGENTGATE_RATE_TENANT_RPS", "0")
				t.Setenv("AGENTGATE_TENANT_MAX_INFLIGHT", "0")
				switch enabled {
				case "rate":
					t.Setenv("AGENTGATE_RATE_TENANT_RPS", "1")
				case "inflight":
					t.Setenv("AGENTGATE_TENANT_MAX_INFLIGHT", "1")
				}
				_, err := loadRateLimits()
				if enabled == "neither" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("expected key limit validation error, got %v", err)
				}
			})
		}
	}
}
