/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

// TestPercentileLedgerAppend is a throwaway measurement harness for the T1
// driver spike (github.com/Clawdlinux/agentgate/pull/20), run once on
// linux/amd64 CI to confirm the darwin/arm64 benchmark generalizes to the
// platform that actually runs in production. Not a permanent test.
func TestPercentileLedgerAppend(t *testing.T) {
	if testing.Short() {
		t.Skip("percentile harness skipped in -short")
	}
	const n = 10000

	database, err := db.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}
	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("signer.NewStore: %v", err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	ledger := NewLedger(database, store)
	draft := testDraft("github", "list_repos")

	durations := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		if _, err := ledger.Append(context.Background(), draft); err != nil {
			t.Fatalf("Append at i=%d: %v", i, err)
		}
		durations = append(durations, time.Since(start).Nanoseconds())
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[n*50/100]
	p90 := durations[n*90/100]
	p99 := durations[n*99/100]
	var sum int64
	for _, d := range durations {
		sum += d
	}
	mean := float64(sum) / float64(n)
	fmt.Printf("PERCENTILE_RESULT os=%s arch=%s n=%d mean_us=%.1f p50_us=%.1f p90_us=%.1f p99_us=%.1f\n",
		runtime.GOOS, runtime.GOARCH, n, mean/1000, float64(p50)/1000, float64(p90)/1000, float64(p99)/1000)

	if raw := os.Getenv("PERCENTILE_RAW_OUT"); raw != "" {
		f, err := os.Create(raw)
		if err != nil {
			t.Fatalf("create raw out: %v", err)
		}
		defer f.Close()
		for _, d := range durations {
			fmt.Fprintln(f, d)
		}
	}
}
