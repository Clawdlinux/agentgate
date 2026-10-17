/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func verifyLedgerChain(t *testing.T, ledger *Ledger, receipts []Receipt) {
	t.Helper()
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Seq < receipts[j].Seq })
	keys, err := ledger.signer.PublicKeys()
	if err != nil {
		t.Fatal(err)
	}
	var trusted []receiptspec.TrustedKey
	for _, key := range keys {
		trusted = append(trusted, receiptspec.TrustedKey{
			KID: key.KID, PublicKey: key.PublicKey,
			ValidFromSeq: key.ValidFromSeq, ValidUntilSeq: key.ValidUntilSeq,
		})
	}
	seq, hash, err := ledger.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiptspec.VerifyChain(receipts, trusted, receiptspec.Anchor{},
		&receiptspec.ExpectedHead{Seq: seq, EntryHash: hash})
	if err != nil || !result.OK || !result.Complete {
		t.Fatalf("VerifyChain: result=%+v err=%v", result, err)
	}
}

func TestLedgerConcurrentGroupCommit(t *testing.T) {
	ledger := newTestLedger(t)
	const workers, appends = 64, 50
	receipts := make([]Receipt, workers*appends)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			for n := range appends {
				index := worker*appends + n
				action := fmt.Sprintf("request_%d", index)
				draft := testDraft("github", action)
				draft.DelegationChain = []string{action}
				r, err := ledger.Append(t.Context(), draft)
				if err != nil {
					t.Error(err)
					return
				}
				if r.Action != action || r.ParamsSHA256 != draft.ParamsSHA256 ||
					len(r.DelegationChain) != 1 || r.DelegationChain[0] != action {
					t.Errorf("wrong receipt for %s", action)
				}
				receipts[index] = r
			}
		})
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	verifyLedgerChain(t, ledger, receipts)
	for i, r := range receipts {
		if r.Seq != uint64(i+1) {
			t.Fatalf("seq = %d, want %d", r.Seq, i+1)
		}
	}
}

func TestLedgerFailedBatchRollsBackEveryWaiter(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(8), WithBatchDelay(time.Hour))
	if _, err := ledger.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON receipts
		WHEN NEW.seq = 4 BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := ledger.Append(t.Context(), testDraft("github", "batch"))
			if !errors.Is(err, ErrLedgerAppend) {
				t.Errorf("batch waiter err = %v", err)
			}
		})
	}
	wg.Wait()
	if seq, _, err := ledger.Head(t.Context()); err != nil || seq != 0 {
		t.Fatalf("head after rollback = %d, err=%v", seq, err)
	}
	if _, err := ledger.db.Exec(`DROP TRIGGER fail_receipt`); err != nil {
		t.Fatal(err)
	}
	receipts := make([]Receipt, 8)
	for i := range 8 {
		wg.Go(func() {
			r, err := ledger.Append(t.Context(), testDraft("github", "recovered"))
			if err != nil {
				t.Error(err)
			}
			receipts[i] = r
		})
	}
	wg.Wait()
	verifyLedgerChain(t, ledger, receipts)
}

func TestLedgerInvalidDraftDoesNotFailValidBatch(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(2), WithBatchDelay(time.Hour))
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Go(func() {
			draft := testDraft("github", "valid")
			if i == 1 {
				draft.PolicyDecision = "invalid"
			}
			_, err := ledger.Append(t.Context(), draft)
			if i == 1 && !errors.Is(err, ErrLedgerAppend) {
				t.Errorf("invalid draft err = %v", err)
			}
			if i != 1 && err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if seq, _, err := ledger.Head(t.Context()); err != nil || seq != 2 {
		t.Fatalf("head = %d, err=%v", seq, err)
	}
}

func TestLedgerRotationKeepsChainVerifiable(t *testing.T) {
	ledger := newTestLedger(t)
	var receipts []Receipt
	for range 10 {
		r, err := ledger.Append(t.Context(), testDraft("github", "before"))
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, r)
	}
	rotated, priv, err := ledger.signer.Rotate(11)
	clear(priv)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	after := make([]Receipt, 64)
	for i := range after {
		wg.Go(func() {
			r, err := ledger.Append(t.Context(), testDraft("github", "after"))
			if err != nil {
				t.Error(err)
			}
			if r.SignerKID != rotated.KID {
				t.Error("receipt after rotation used old key")
			}
			after[i] = r
		})
	}
	wg.Wait()
	verifyLedgerChain(t, ledger, append(receipts, after...))
}

func TestLedgerCancelBeforeSQLiteDropsRequest(t *testing.T) {
	base := newTestLedger(t)
	synctest.Test(t, func(t *testing.T) {
		ledger := NewLedger(base.db, base.signer, WithBatchDelay(time.Hour))
		defer ledger.Close()
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := ledger.Append(ctx, testDraft("github", "cancelled"))
			result <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-result; !errors.Is(err, ErrLedgerAppend) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel err = %v", err)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
	})
	if seq, _, err := base.Head(t.Context()); err != nil || seq != 0 {
		t.Fatalf("cancelled request head = %d, err=%v", seq, err)
	}
}

func TestLedgerCloseDrainsAndRejectsPending(t *testing.T) {
	base := newTestLedger(t)
	synctest.Test(t, func(t *testing.T) {
		ledger := NewLedger(base.db, base.signer, WithBatchDelay(time.Hour))
		defer ledger.Close()
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				_, err := ledger.Append(t.Context(), testDraft("github", "pending"))
				if !errors.Is(err, ErrLedgerAppend) || !errors.Is(err, ErrLedgerClosed) {
					t.Errorf("pending append err = %v", err)
				}
			})
		}
		synctest.Wait()
		ledger.Close()
		ledger.Close()
		wg.Wait()
		if _, err := ledger.Append(t.Context(), testDraft("github", "closed")); !errors.Is(err, ErrLedgerClosed) {
			t.Fatalf("append after Close: %v", err)
		}
	})
	if seq, _, err := base.Head(t.Context()); err != nil || seq != 0 {
		t.Fatalf("head after Close = %d, err=%v", seq, err)
	}
}

func TestLedgerCancelAfterSQLiteCanCommit(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(1))
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	ledger.nowFn = func() time.Time {
		close(entered)
		<-release
		return time.Now()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	draft := testDraft("github", "cancel_after_submission")
	draft.DelegationChain = []string{"original"}
	result := make(chan error, 1)
	go func() {
		_, err := ledger.Append(ctx, draft)
		result <- err
	}()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, ErrLedgerAppend) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err = %v", err)
	}
	draft.DelegationChain[0] = "changed_after_return"
	unblock.Do(func() { close(release) })
	ledger.Close()
	r, err := ScanReceiptRow(ledger.db.QueryRow(`SELECT ` + receiptColumns + ` FROM receipts WHERE seq = 1`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.DelegationChain) != 1 || r.DelegationChain[0] != "original" {
		t.Fatal("queued draft was modified after Append returned")
	}
	verifyLedgerChain(t, ledger, []Receipt{r})
}

func TestLedgerRotationWaitsForSubmittedBatch(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(1))
	entered, release := make(chan struct{}), make(chan struct{})
	var first, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	ledger.nowFn = func() time.Time {
		first.Do(func() {
			close(entered)
			<-release
		})
		return time.Now()
	}
	committed := make(chan appendResult, 1)
	go func() {
		r, err := ledger.Append(t.Context(), testDraft("github", "before"))
		committed <- appendResult{receipt: r, err: err}
	}()
	<-entered
	rotated := make(chan error, 1)
	go func() {
		_, priv, err := ledger.signer.Rotate(2)
		clear(priv)
		rotated <- err
	}()
	unblock.Do(func() { close(release) })
	before := <-committed
	if before.err != nil {
		t.Fatal(before.err)
	}
	if err := <-rotated; err != nil {
		t.Fatal(err)
	}
	after, err := ledger.Append(t.Context(), testDraft("github", "after"))
	if err != nil {
		t.Fatal(err)
	}
	if before.receipt.SignerKID == after.SignerKID {
		t.Fatal("rotation did not change the signing key")
	}
	verifyLedgerChain(t, ledger, []Receipt{before.receipt, after})
}

func TestLedgerOptionsAndCloseBeforeStart(t *testing.T) {
	ledger := NewLedger(nil, nil, WithBatchSize(0), WithBatchDelay(-time.Second))
	if ledger.batchSize != 256 || ledger.batchDelay != 0 {
		t.Fatal("invalid options replaced defaults")
	}
	ledger.Close()
	ledger.Close()
	if _, err := ledger.Append(t.Context(), testDraft("github", "closed")); !errors.Is(err, ErrLedgerAppend) {
		t.Fatalf("closed ledger err = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ledger.Append(cancelled, testDraft("github", "cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled append err = %v", err)
	}
}

func TestLedgerLoneAppendCollectionDelay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []LedgerOption
		wait    time.Duration
	}{
		{name: "default"},
		{name: "explicit_zero", options: []LedgerOption{WithBatchDelay(0)}},
		{name: "configured_window", options: []LedgerOption{WithBatchDelay(7 * time.Millisecond)}, wait: 7 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := newTestLedger(t)
			synctest.Test(t, func(t *testing.T) {
				ledger := NewLedger(base.db, base.signer, tc.options...)
				defer ledger.Close()
				start := time.Now()
				r, err := ledger.Append(t.Context(), testDraft("github", "lone"))
				if err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != tc.wait {
					t.Fatalf("collection delay = %v, want %v", elapsed, tc.wait)
				}
				if r.Seq != 1 {
					t.Fatalf("receipt seq = %d, want 1", r.Seq)
				}
			})
		})
	}
}

func TestLedgerNoWaitDrainsReadyRequestsUpToCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ledger := NewLedger(nil, nil, WithBatchSize(3))
		defer ledger.Close()
		ledger.requests = make(chan appendRequest, 5)
		for i := range 5 {
			ledger.requests <- appendRequest{draft: Draft{Action: fmt.Sprintf("queued_%d", i)}}
		}
		start := time.Now()
		first := ledger.collectBatch(<-ledger.requests)
		if len(first) != 3 || len(ledger.requests) != 2 {
			t.Fatalf("batch size = %d, pending = %d", len(first), len(ledger.requests))
		}
		second := ledger.collectBatch(<-ledger.requests)
		if len(second) != 2 || len(ledger.requests) != 0 {
			t.Fatalf("next batch size = %d, pending = %d", len(second), len(ledger.requests))
		}
		for i, request := range append(first, second...) {
			if want := fmt.Sprintf("queued_%d", i); request.draft.Action != want {
				t.Fatalf("request action = %s, want %s", request.draft.Action, want)
			}
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("ready requests waited %v", elapsed)
		}
	})
}

func TestLedgerRejectsEveryInvalidEncodingBeforeBatch(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(1))
	for _, mutate := range []func(*Draft){
		func(d *Draft) { d.Service = strings.Repeat("s", 65) },
		func(d *Draft) { d.DelegationChain = []string{"non_ascii_é"} },
		func(d *Draft) { d.StatusCode = 600 },
		func(d *Draft) { d.Error = "invalid-error" },
		func(d *Draft) { d.LatencyMS = -1 },
	} {
		draft := testDraft("github", "invalid")
		mutate(&draft)
		if _, err := ledger.Append(t.Context(), draft); !errors.Is(err, ErrLedgerAppend) {
			t.Fatalf("invalid encoding err = %v", err)
		}
	}
	if seq, _, err := ledger.Head(t.Context()); err != nil || seq != 0 {
		t.Fatalf("head = %d, err=%v", seq, err)
	}
}

func TestLedgerRotationRejectsStaleOrFutureBoundary(t *testing.T) {
	ledger := newTestLedger(t, WithBatchSize(1))
	receipts := appendN(t, ledger, 3)
	for _, seq := range []uint64{2, 5} {
		_, priv, err := ledger.signer.Rotate(seq)
		clear(priv)
		if !errors.Is(err, signer.ErrRotationSequence) {
			t.Fatalf("rotation at %d: %v, want ErrRotationSequence", seq, err)
		}
		verifyLedgerChain(t, ledger, receipts)
	}
	_, priv, err := ledger.signer.Rotate(4)
	clear(priv)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ledger.Append(t.Context(), testDraft("github", "rotated"))
	if err != nil {
		t.Fatal(err)
	}
	verifyLedgerChain(t, ledger, append(receipts, after))
}

func TestLedgerInitializesSignerWithoutWriteLockDeadlock(t *testing.T) {
	base := newTestLedger(t)
	base.signer.Close()
	if _, err := base.db.Exec(`DELETE FROM signer_keys`); err != nil {
		t.Fatal(err)
	}
	base.db.SetMaxOpenConns(1)
	store, err := signer.NewStore(base.db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ledger := NewLedger(base.db, store, WithBatchSize(1))
	defer ledger.Close()
	if _, err := ledger.Append(t.Context(), testDraft("github", "cold_signer")); err != nil {
		t.Fatal(err)
	}
}
