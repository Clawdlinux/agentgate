package main

import (
	"os"
	"testing"
	"time"
)

func TestLoadVaultCacheTTL(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
		fail  bool
	}{
		{"60s", time.Minute, false},
		{"5m", 5 * time.Minute, false},
		{"0", 0, false},
		{"0s", 0, false},
		{"-1s", 0, true},
		{"invalid", 0, true},
		{"", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AGENTGATE_VAULT_CACHE_TTL", tc.value)
			got, err := loadVaultCacheTTL()
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("TTL = %v, err = %v", got, err)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		t.Setenv("AGENTGATE_VAULT_CACHE_TTL", "")
		if err := os.Unsetenv("AGENTGATE_VAULT_CACHE_TTL"); err != nil {
			t.Fatal(err)
		}
		got, err := loadVaultCacheTTL()
		if err != nil || got != time.Minute {
			t.Fatalf("default TTL = %v, err = %v", got, err)
		}
	})
}
