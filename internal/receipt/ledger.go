/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/trace"
	"slices"
	"sync"
	"time"

	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

// ErrLedgerAppend wraps every Append failure.
// Callers must not return successful action responses on failure (LEDG-07).
// Cancellation after SQLite submission can still leave a committed receipt.
var ErrLedgerAppend = errors.New("receipt: ledger append failed")

// ErrLedgerClosed means the writer no longer accepts requests.
var ErrLedgerClosed = errors.New("receipt: ledger closed")

// Draft is the caller-supplied input to Append. The ledger itself computes
// Seq, TimestampUnixNS, PrevHash, EntryHash, SignerKID, and Signature. A
// caller cannot forge or skip sequencing or signing.
type Draft struct {
	HumanPrincipal  string
	AgentKeyID      string
	DelegationChain []string
	Service         string
	Action          string
	ParamsSHA256    [32]byte
	PolicyDecision  string
	StatusCode      int
	LatencyMS       int64
	Error           string
	// Record is stored atomically beside the receipt, not inside its wire format.
	// Its canonical hash must equal ParamsSHA256.
	Record json.RawMessage
}

// Ledger batches appends through a single writer (LEDG-06).
// Each batch reads SQLite's committed head inside one BEGIN IMMEDIATE transaction.
// Signing follows that read. All inserts commit before any success reply.
// SQLite remains the source of truth across restarts (LEDG-08).
// Committed sequences have no gaps. Failed transactions consume no sequence.
//
// Crash and completeness limits (LEDG-11): there is no atomic transaction
// spanning SQLite and an external SaaS side effect. A SaaS action can
// complete before a later receipt append fails, and that failure is not
// otherwise recorded against the SaaS side effect. This ledger guarantees
// only that (a) no successful HTTP action response is returned before its
// receipt commits, and (b) committed sequences contain no allocation gaps.
// It does not guarantee exactly-once evidence under disk failure; that
// requires an intent/completion protocol outside this schema.
type Ledger struct {
	db     *sql.DB
	signer *signer.Store
	nowFn  func() time.Time

	batchSize  int
	batchDelay time.Duration
	requests   chan appendRequest
	stop       chan struct{}
	done       chan struct{}
	start      sync.Once
	stopOnce   sync.Once
}

type appendRequest struct {
	ctx   context.Context
	draft Draft
	reply chan appendResult
}

type appendResult struct {
	receipt Receipt
	err     error
}

// LedgerOption configures batching before the writer starts.
type LedgerOption func(*Ledger)

// WithBatchSize sets the maximum receipts per transaction. Nonpositive values keep the default.
func WithBatchSize(size int) LedgerOption {
	return func(l *Ledger) {
		if size > 0 {
			l.batchSize = size
		}
	}
}

// WithBatchDelay sets the maximum collection wait. Zero drains ready requests without a timer.
// Negative values keep the default.
func WithBatchDelay(delay time.Duration) LedgerOption {
	return func(l *Ledger) {
		if delay >= 0 {
			l.batchDelay = delay
		}
	}
}

// NewLedger constructs a Ledger backed by db and signerStore. db must
// already have migration 002_receipts.sql applied.
// The writer starts on the first valid Append. Defaults are no collection wait and 256 entries.
func NewLedger(db *sql.DB, signerStore *signer.Store, options ...LedgerOption) *Ledger {
	l := &Ledger{
		db: db, signer: signerStore, nowFn: time.Now,
		batchSize: 256,
		requests:  make(chan appendRequest), stop: make(chan struct{}), done: make(chan struct{}),
	}
	for _, option := range options {
		option(l)
	}
	return l
}

// Close rejects pending requests and waits for the writer to stop.
// An already submitted transaction finishes before Close returns.
// Stop HTTP first, then close the ledger, signer, and database.
func (l *Ledger) Close() {
	l.stopOnce.Do(func() { close(l.stop) })
	l.start.Do(func() { close(l.done) })
	<-l.done
}

// Head returns the current committed sequence number and entry hash, or
// (0, zero-hash) if the ledger is empty. It always reads from SQLite.
func (l *Ledger) Head(ctx context.Context) (uint64, [32]byte, error) {
	return readHead(ctx, l.db)
}

// Append waits for its receipt's batch to commit.
// Invalid drafts fail before SQLite and cannot fail their valid batch peers.
// After acquiring SQLite's write lock and reading the head, the writer drops cancelled requests.
// Cancellation after that check stops waiting but the receipt may still commit.
// Every returned error wraps ErrLedgerAppend.
func (l *Ledger) Append(ctx context.Context, draft Draft) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, appendError(err)
	}
	select {
	case <-l.stop:
		return Receipt{}, appendError(ErrLedgerClosed)
	default:
	}
	draft.DelegationChain = slices.Clone(draft.DelegationChain)
	preflight := draft.receipt()
	preflight.Seq, preflight.TimestampUnixNS, preflight.SignerKID = 1, 1, "preflight"
	if err := receiptspec.Validate(preflight); err != nil {
		return Receipt{}, appendError(err)
	}
	if len(draft.Record) > 0 {
		canonical, err := receiptspec.CanonicalRecord(draft.Record)
		if err != nil {
			return Receipt{}, appendError(err)
		}
		if err := receiptspec.VerifyRecordBinding(preflight, canonical); err != nil {
			return Receipt{}, appendError(err)
		}
		draft.Record = canonical
	}
	request := appendRequest{ctx: ctx, draft: draft, reply: make(chan appendResult, 1)}
	l.start.Do(func() { go l.run() })
	select {
	case l.requests <- request:
	case <-ctx.Done():
		return Receipt{}, appendError(ctx.Err())
	case <-l.stop:
		return Receipt{}, appendError(ErrLedgerClosed)
	}
	select {
	case result := <-request.reply:
		return result.receipt, result.err
	case <-ctx.Done():
		return Receipt{}, appendError(ctx.Err())
	}
}

func appendError(err error) error {
	return fmt.Errorf("%w: %w", ErrLedgerAppend, err)
}

// run deliberately has no recover. A writer panic terminates the process.
// Stack unwinding rolls back open transactions. Success replies require COMMIT, so panics cannot report uncommitted receipts.
func (l *Ledger) run() {
	defer close(l.done)
	for {
		var first appendRequest
		select {
		case <-l.stop:
			return
		case first = <-l.requests:
		}
		l.flush(l.collectBatch(first))
	}
}

func (l *Ledger) collectBatch(first appendRequest) []appendRequest {
	batch := []appendRequest{first}
	if l.batchDelay == 0 {
		for len(batch) < l.batchSize {
			select {
			case <-l.stop:
				return batch
			case request := <-l.requests:
				batch = append(batch, request)
			default:
				return batch
			}
		}
		return batch
	}
	timer := time.NewTimer(l.batchDelay)
	defer timer.Stop()
	for len(batch) < l.batchSize {
		select {
		case <-l.stop:
			return batch
		case request := <-l.requests:
			batch = append(batch, request)
		case <-timer.C:
			return batch
		}
	}
	return batch
}

func (l *Ledger) flush(batch []appendRequest) {
	var results []appendResult
	// The lease excludes rotation through the entire transaction, including COMMIT.
	// Acquire it before SQLite's lock to avoid lock inversion with Rotate.
	err := l.signer.WithActiveKey(1, func(key signer.KeyRecord, priv ed25519.PrivateKey) error {
		select {
		case <-l.stop:
			return ErrLedgerClosed
		default:
		}
		var err error
		results, err = l.commitBatch(batch, key, priv)
		return err
	})
	for i, request := range batch {
		if trace.IsEnabled() {
			trace.Log(request.ctx, "receipt.reply", "")
		}
		if err != nil {
			request.reply <- appendResult{err: appendError(err)}
		} else {
			request.reply <- results[i]
		}
	}
}

func (l *Ledger) commitBatch(batch []appendRequest, key signer.KeyRecord, priv ed25519.PrivateKey) ([]appendResult, error) {
	// Caller cancellation must not interrupt a shared transaction.
	ctx := context.Background()
	region := trace.StartRegion(ctx, "receipt.transaction")
	defer region.End()
	if trace.IsEnabled() {
		trace.Logf(ctx, "receipt.batch", "%d", len(batch))
	}
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
				// Never return a connection with an open transaction to the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
		}
	}()

	headRegion := trace.StartRegion(ctx, "receipt.head")
	head, prevHash, err := readHead(ctx, conn)
	headRegion.End()
	if err != nil {
		return nil, fmt.Errorf("read head: %w", err)
	}
	// BEGIN can wait on another writer. Check deadlines only after that wait and the head read.
	results := make([]appendResult, len(batch))
	active := 0
	for i, request := range batch {
		if err := request.ctx.Err(); err != nil {
			results[i].err = appendError(err)
		} else {
			active++
		}
	}
	if active == 0 {
		return results, nil
	}
	if head+1 < key.ValidFromSeq {
		return nil, errors.New("active signing key starts after the next receipt")
	}
	for i, request := range batch {
		if results[i].err != nil {
			continue
		}
		r := request.draft.receipt()
		r.Seq, r.PrevHash, r.TimestampUnixNS = head+1, prevHash, uint64(l.nowFn().UnixNano())
		signRegion := trace.StartRegion(ctx, "receipt.sign")
		r, err = receiptspec.Seal(r, storeSigner{kid: key.KID, priv: priv})
		signRegion.End()
		if err != nil {
			return nil, fmt.Errorf("sign receipt: %w", err)
		}
		insertRegion := trace.StartRegion(ctx, "receipt.insert")
		err = insertReceipt(ctx, conn, r)
		insertRegion.End()
		if err != nil {
			return nil, fmt.Errorf("insert: %w", err)
		}
		if len(request.draft.Record) > 0 {
			if _, err := conn.ExecContext(ctx, "INSERT INTO receipt_records (seq, record_json) VALUES (?, ?)",
				r.Seq, []byte(request.draft.Record)); err != nil {
				return nil, fmt.Errorf("insert evidence record: %w", err)
			}
		}
		results[i].receipt = r
		head, prevHash = r.Seq, r.EntryHash
	}

	commitRegion := trace.StartRegion(ctx, "receipt.commit")
	_, err = conn.ExecContext(ctx, "COMMIT")
	commitRegion.End()
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return results, nil
}

// Record returns the hash-bound evidence for a receipt. Missing records return sql.ErrNoRows.
func (l *Ledger) Record(ctx context.Context, seq uint64) (json.RawMessage, error) {
	var record []byte
	var digest []byte
	err := l.db.QueryRowContext(ctx, `SELECT e.record_json, r.params_sha256
		FROM receipt_records e JOIN receipts r ON r.seq=e.seq WHERE e.seq=?`, seq).Scan(&record, &digest)
	if err != nil {
		return nil, err
	}
	if len(digest) != 32 {
		return nil, errors.New("receipt: invalid evidence binding digest")
	}
	var r Receipt
	copy(r.ParamsSHA256[:], digest)
	if err := receiptspec.VerifyRecordBinding(r, record); err != nil {
		return nil, err
	}
	return json.RawMessage(record), nil
}

func (d Draft) receipt() Receipt {
	return Receipt{
		HumanPrincipal: d.HumanPrincipal, AgentKeyID: d.AgentKeyID,
		DelegationChain: d.DelegationChain, Service: d.Service, Action: d.Action,
		ParamsSHA256: d.ParamsSHA256, PolicyDecision: d.PolicyDecision,
		StatusCode: d.StatusCode, LatencyMS: d.LatencyMS, Error: d.Error,
	}
}

// storeSigner signs with the signer store's active key under its stored KID.
type storeSigner struct {
	kid  string
	priv ed25519.PrivateKey
}

func (s storeSigner) KID() string { return s.kid }

func (s storeSigner) Sign(msg []byte) ([]byte, error) {
	sig := signer.Sign(s.priv, msg)
	return sig[:], nil
}

// headReader is satisfied by both *sql.DB and *sql.Conn, so readHead can
// run either as a standalone query (Head) or inside an open transaction on
// a pinned connection (Append).
type headReader interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readHead(ctx context.Context, r headReader) (uint64, [32]byte, error) {
	row := r.QueryRowContext(ctx, `SELECT seq, entry_hash FROM receipts ORDER BY seq DESC LIMIT 1`)
	var seq uint64
	var hash []byte
	if err := row.Scan(&seq, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, [32]byte{}, nil
		}
		return 0, [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], hash)
	return seq, out, nil
}

func insertReceipt(ctx context.Context, conn *sql.Conn, r Receipt) error {
	chain := r.DelegationChain
	if chain == nil {
		chain = []string{}
	}
	chainJSON, err := json.Marshal(chain)
	if err != nil {
		return err
	}

	_, err = conn.ExecContext(ctx, `
		INSERT INTO receipts (
			seq, format_version, timestamp_unix_ns, human_principal, agent_key_id,
			delegation_chain_json, service, action, params_sha256, policy_decision,
			status_code, latency_ms, error_code, prev_hash, entry_hash, signer_kid, signature
		) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Seq, r.TimestampUnixNS, r.HumanPrincipal, r.AgentKeyID, string(chainJSON),
		r.Service, r.Action, r.ParamsSHA256[:], r.PolicyDecision,
		r.StatusCode, r.LatencyMS, r.Error, r.PrevHash[:], r.EntryHash[:], r.SignerKID, r.Signature[:],
	)
	return err
}
