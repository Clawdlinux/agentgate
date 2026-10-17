package main

import (
	"os"
	"testing"
	"time"
)

func TestLoadAPIKeyCacheTTL(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
		fail  bool
	}{
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"0", 0, false},
		{"0s", 0, false},
		{"-1s", 0, true},
		{"invalid", 0, true},
		{"", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AGENTGATE_API_KEY_CACHE_TTL", tc.value)
			got, err := loadAPIKeyCacheTTL()
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("TTL = %v, err = %v", got, err)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		t.Setenv("AGENTGATE_API_KEY_CACHE_TTL", "")
		if err := os.Unsetenv("AGENTGATE_API_KEY_CACHE_TTL"); err != nil {
			t.Fatal(err)
		}
		got, err := loadAPIKeyCacheTTL()
		if err != nil || got != 30*time.Second {
			t.Fatalf("default TTL = %v, err = %v", got, err)
		}
	})
}
