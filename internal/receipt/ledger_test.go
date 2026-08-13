/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

func testMasterKey() []byte {
	return []byte("01234567890123456789012345678901")
}

func newTestLedger(t *testing.T) *Ledger {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
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
	return NewLedger(database, store)
}

func testDraft(service, action string) Draft {
	digest := sha256.Sum256([]byte(service + action))
	return Draft{
		HumanPrincipal: "user-1",
		AgentKeyID:     "agent-1",
		Service:        service,
		Action:         action,
		ParamsSHA256:   digest,
		PolicyDecision: "allow",
		StatusCode:     200,
		LatencyMS:      12,
	}
}

func TestLedgerAppend_EmptyLedgerStartsAtSeqOne(t *testing.T) {
	t.Parallel()
	ledger := newTestLedger(t)

	seq, hash, err := ledger.Head(t.Context())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if seq != 0 || hash != ([32]byte{}) {
		t.Fatalf("Head = (%d, %x), want (0, zero)", seq, hash)
	}

	r, err := ledger.Append(t.Context(), testDraft("github", "list_repos"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if r.Seq != 1 {
		t.Fatalf("Seq = %d, want 1", r.Seq)
	}
	if r.PrevHash != ([32]byte{}) {
		t.Fatalf("PrevHash = %x, want zero for the first receipt", r.PrevHash)
	}
}

func TestLedgerAppend_ChainsPrevHashToPriorEntryHash(t *testing.T) {
	t.Parallel()
	ledger := newTestLedger(t)

	first, err := ledger.Append(t.Context(), testDraft("github", "list_repos"))
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	second, err := ledger.Append(t.Context(), testDraft("stripe", "list_invoices"))
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	if second.Seq != 2 {
		t.Fatalf("second.Seq = %d, want 2", second.Seq)
	}
	if second.PrevHash != first.EntryHash {
		t.Fatalf("second.PrevHash = %x, want first.EntryHash = %x", second.PrevHash, first.EntryHash)
	}
}

func TestLedgerAppend_SignatureVerifiesUnderPublishedPublicKey(t *testing.T) {
	t.Parallel()
	database, err := db.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
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

	r, err := ledger.Append(t.Context(), testDraft("github", "list_repos"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	keys, err := store.PublicKeys()
	if err != nil {
		t.Fatalf("PublicKeys: %v", err)
	}
	var pub = findKey(t, keys, r.SignerKID)

	if !signer.Verify(pub, r.EntryHash[:], r.Signature) {
		t.Fatal("signature did not verify under the published public key")
	}
}

func findKey(t *testing.T, keys []signer.KeyRecord, kid string) []byte {
	t.Helper()
	for _, k := range keys {
		if k.KID == kid {
			return k.PublicKey
		}
	}
	t.Fatalf("kid %s not found among published keys", kid)
	return nil
}

func TestLedgerAppend_InvalidDraftConsumesNoSequence(t *testing.T) {
	t.Parallel()
	ledger := newTestLedger(t)

	if _, err := ledger.Append(t.Context(), testDraft("github", "list_repos")); err != nil {
		t.Fatalf("seed Append: %v", err)
	}

	bad := testDraft("github", "list_repos")
	bad.PolicyDecision = "not-a-real-decision" // fails receipt.Validate inside ComputeEntryHash

	if _, err := ledger.Append(t.Context(), bad); !errors.Is(err, ErrLedgerAppend) {
		t.Fatalf("Append with invalid draft: err = %v, want ErrLedgerAppend", err)
	}

	seq, _, err := ledger.Head(t.Context())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if seq != 1 {
		t.Fatalf("Head seq = %d after a failed append, want 1 (no sequence consumed)", seq)
	}

	// The chain still accepts further valid appends at seq 2 — the failed
	// attempt left no gap and no partial row.
	next, err := ledger.Append(t.Context(), testDraft("stripe", "list_invoices"))
	if err != nil {
		t.Fatalf("Append after failure: %v", err)
	}
	if next.Seq != 2 {
		t.Fatalf("next.Seq = %d, want 2", next.Seq)
	}
}

func TestLedgerAppend_RestartResumesFromCommittedHead(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "agentgate.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
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

	for i := 0; i < 3; i++ {
		if _, err := ledger.Append(t.Context(), testDraft("github", "list_repos")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	database.Close()

	// "Restart": brand new db handle, store, and ledger over the same file.
	restarted, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open (restart): %v", err)
	}
	t.Cleanup(func() { restarted.Close() })
	restartedStore, err := signer.NewStore(restarted, testMasterKey())
	if err != nil {
		t.Fatalf("signer.NewStore (restart): %v", err)
	}
	if _, _, err := restartedStore.LoadOrCreateActive(1); err != nil {
		t.Fatalf("LoadOrCreateActive (restart): %v", err)
	}
	restartedLedger := NewLedger(restarted, restartedStore)

	seq, _, err := restartedLedger.Head(t.Context())
	if err != nil {
		t.Fatalf("Head (restart): %v", err)
	}
	if seq != 3 {
		t.Fatalf("Head seq after restart = %d, want 3 (from disk, not memory)", seq)
	}

	r, err := restartedLedger.Append(t.Context(), testDraft("github", "list_repos"))
	if err != nil {
		t.Fatalf("Append (restart): %v", err)
	}
	if r.Seq != 4 {
		t.Fatalf("Seq after restart append = %d, want 4", r.Seq)
	}
}

// TestLedgerAppend_HundredConcurrentAppendsProduceExactSequence1To100
// covers LEDG-09: one hundred concurrent actions produce a chain
// containing exactly sequences 1 through 100, with no gaps and no
// duplicates.
func TestLedgerAppend_HundredConcurrentAppendsProduceExactSequence1To100(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	ledger := newTestLedger(t)

	const n = 100
	var wg sync.WaitGroup
	seqs := make([]uint64, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := ledger.Append(context.Background(), testDraft("github", "list_repos"))
			seqs[i] = r.Seq
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]bool, n)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if seen[seqs[i]] {
			t.Fatalf("duplicate sequence %d", seqs[i])
		}
		seen[seqs[i]] = true
	}
	for want := uint64(1); want <= n; want++ {
		if !seen[want] {
			t.Fatalf("missing sequence %d out of 1..%d", want, n)
		}
	}

	head, _, err := ledger.Head(t.Context())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != n {
		t.Fatalf("Head = %d, want %d", head, n)
	}
}

// BenchmarkLedgerAppend measures single-writer append latency, recorded
// for LEDG-10 (p99 receipt latency).
func BenchmarkLedgerAppend(b *testing.B) {
	database, err := db.Open(filepath.Join(b.TempDir(), "agentgate.db"))
	if err != nil {
		b.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		b.Fatalf("db.RunMigrations: %v", err)
	}
	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		b.Fatalf("signer.NewStore: %v", err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		b.Fatalf("LoadOrCreateActive: %v", err)
	}
	ledger := NewLedger(database, store)
	draft := testDraft("github", "list_repos")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ledger.Append(context.Background(), draft); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}
