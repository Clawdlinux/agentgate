/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

// ErrLedgerAppend wraps every failure returned by Ledger.Append. Callers
// should treat any error from Append as "no receipt was committed" — the
// caller must not return a successful action response in that case
// (LEDG-07).
var ErrLedgerAppend = errors.New("receipt: ledger append failed")

// Draft is the caller-supplied input to Append. The ledger itself computes
// Seq, TimestampUnixNS, PrevHash, EntryHash, SignerKID, and Signature — a
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
}

// Ledger owns sequence allocation, predecessor lookup, signing, and
// persistence for the receipt chain, as one serialized SQLite transaction
// per append (LEDG-06). It has no in-memory source of truth: every Append
// and Head call reads the committed state directly from SQLite, so a
// process restart resumes from exactly the committed head with no gaps or
// duplicates (LEDG-08).
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
}

// NewLedger constructs a Ledger backed by db and signerStore. db must
// already have migration 002_receipts.sql applied.
func NewLedger(db *sql.DB, signerStore *signer.Store) *Ledger {
	return &Ledger{db: db, signer: signerStore, nowFn: time.Now}
}

// Head returns the current committed sequence number and entry hash, or
// (0, zero-hash) if the ledger is empty. It always reads from SQLite.
func (l *Ledger) Head(ctx context.Context) (uint64, [32]byte, error) {
	return readHead(ctx, l.db)
}

// Append signs and durably commits one receipt for draft inside a single
// BEGIN IMMEDIATE transaction, returning the committed receipt. BEGIN
// IMMEDIATE acquires SQLite's write lock before any read, so no other
// writer can observe or act on a stale head between this transaction's
// head-read and its insert — the ordinary pooled db.Begin() only upgrades
// to a write lock at the first write statement, which would leave that
// race open. On any failure the transaction rolls back and no sequence
// number is consumed (LEDG-07).
func (l *Ledger) Append(ctx context.Context, draft Draft) (Receipt, error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: acquire connection: %v", ErrLedgerAppend, err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return Receipt{}, fmt.Errorf("%w: begin: %v", ErrLedgerAppend, err)
	}
	committed := false
	defer func() {
		if !committed {
			// Best-effort rollback on a fresh context: ctx may already be
			// canceled or expired by the time a prior step failed.
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	head, prevHash, err := readHead(ctx, conn)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: read head: %v", ErrLedgerAppend, err)
	}

	keyRecord, priv, err := l.signer.LoadOrCreateActive(1)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: signer key: %v", ErrLedgerAppend, err)
	}

	r := Receipt{
		Seq:             head + 1,
		TimestampUnixNS: uint64(l.nowFn().UnixNano()),
		HumanPrincipal:  draft.HumanPrincipal,
		AgentKeyID:      draft.AgentKeyID,
		DelegationChain: draft.DelegationChain,
		Service:         draft.Service,
		Action:          draft.Action,
		ParamsSHA256:    draft.ParamsSHA256,
		PolicyDecision:  draft.PolicyDecision,
		StatusCode:      draft.StatusCode,
		LatencyMS:       draft.LatencyMS,
		Error:           draft.Error,
		PrevHash:        prevHash,
		SignerKID:       keyRecord.KID,
	}

	entryHash, err := ComputeEntryHash(r)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: compute hash: %v", ErrLedgerAppend, err)
	}
	r.EntryHash = entryHash
	r.Signature = signer.Sign(priv, entryHash[:])

	if err := insertReceipt(ctx, conn, r); err != nil {
		return Receipt{}, fmt.Errorf("%w: insert: %v", ErrLedgerAppend, err)
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return Receipt{}, fmt.Errorf("%w: commit: %v", ErrLedgerAppend, err)
	}
	committed = true
	return r, nil
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
