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
	"net/http"
	"strconv"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

// MaxExportRange caps the number of receipts one export request may
// return. A caller asking for more gets 400, not a silently truncated
// response.
const MaxExportRange = 10000

const receiptColumns = `seq, format_version, timestamp_unix_ns, human_principal, agent_key_id,
	delegation_chain_json, service, action, params_sha256, policy_decision,
	status_code, latency_ms, error_code, prev_hash, entry_hash, signer_kid, signature`

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// ScanReceiptRow scans one row selected with receiptColumns into a
// Receipt. Shared by the export handler and cmd/agentgate-verify's SQLite
// reader so both use exactly one column-to-field mapping.
func ScanReceiptRow(row rowScanner) (Receipt, error) {
	var r Receipt
	var formatVersion int
	var delegationJSON string
	var paramsSHA256, prevHash, entryHash, signature []byte

	if err := row.Scan(&r.Seq, &formatVersion, &r.TimestampUnixNS, &r.HumanPrincipal, &r.AgentKeyID,
		&delegationJSON, &r.Service, &r.Action, &paramsSHA256, &r.PolicyDecision,
		&r.StatusCode, &r.LatencyMS, &r.Error, &prevHash, &entryHash, &r.SignerKID, &signature); err != nil {
		return Receipt{}, err
	}
	if formatVersion != JSONLFormatVersion {
		return Receipt{}, fmt.Errorf("%w: seq %d has format_version %d", ErrUnsupportedFormatVersion, r.Seq, formatVersion)
	}
	if err := json.Unmarshal([]byte(delegationJSON), &r.DelegationChain); err != nil {
		return Receipt{}, fmt.Errorf("seq %d: delegation_chain_json: %w", r.Seq, err)
	}
	for _, f := range []struct {
		name string
		src  []byte
		dst  []byte
	}{
		{"params_sha256", paramsSHA256, r.ParamsSHA256[:]},
		{"prev_hash", prevHash, r.PrevHash[:]},
		{"entry_hash", entryHash, r.EntryHash[:]},
		{"signature", signature, r.Signature[:]},
	} {
		if len(f.src) != len(f.dst) {
			return Receipt{}, fmt.Errorf("seq %d: %s: want %d bytes, got %d", r.Seq, f.name, len(f.dst), len(f.src))
		}
		copy(f.dst, f.src)
	}
	return r, nil
}

// ExportHandler serves GET /v1/receipts/export?from=&to=: a signed,
// snapshot-consistent, sequence-ordered JSONL export. Mount it behind an
// admin-auth middleware — it performs no authorization itself.
func ExportHandler(db *sql.DB, signerStore *signer.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, requestedTo, hasTo, ok := parseExportRange(w, r)
		if !ok {
			return
		}

		ctx := r.Context()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			writeExportError(w, http.StatusInternalServerError, "snapshot failed")
			return
		}
		defer tx.Rollback()

		headSeq, headHash, err := readHead(ctx, tx)
		if err != nil {
			writeExportError(w, http.StatusInternalServerError, "read head failed")
			return
		}

		resolvedTo, errMsg, ok := resolveExportRange(from, requestedTo, hasTo, headSeq)
		if !ok {
			writeExportError(w, http.StatusBadRequest, errMsg)
			return
		}

		anchorSeq := from - 1
		var anchorHash [32]byte
		if anchorSeq > 0 {
			anchorHash, err = queryEntryHashAt(ctx, tx, anchorSeq)
			if err != nil {
				writeExportError(w, http.StatusInternalServerError, "read anchor failed")
				return
			}
		}

		receipts, err := queryReceiptRange(ctx, tx, from, resolvedTo)
		if err != nil {
			writeExportError(w, http.StatusInternalServerError, "read receipts failed")
			return
		}
		keys, err := queryAllSignerKeys(ctx, tx)
		if err != nil {
			writeExportError(w, http.StatusInternalServerError, "read keys failed")
			return
		}
		if err := tx.Commit(); err != nil {
			writeExportError(w, http.StatusInternalServerError, "snapshot commit failed")
			return
		}

		firstHash, lastHash := anchorHash, anchorHash
		if len(receipts) > 0 {
			firstHash = receipts[0].EntryHash
			lastHash = receipts[len(receipts)-1].EntryHash
		}

		manifest := ExportManifest{
			FormatVersion:  ExportFormatVersion,
			RequestedFrom:  from,
			RequestedTo:    requestedTo,
			ResolvedTo:     resolvedTo,
			Count:          len(receipts),
			AnchorSeq:      anchorSeq,
			AnchorHash:     anchorHash,
			FirstEntryHash: firstHash,
			LastEntryHash:  lastHash,
			HeadSeq:        headSeq,
			HeadHash:       headHash,
			KeysetDigest:   ComputeKeysetDigest(keys),
		}

		activeKey, priv, err := signerStore.LoadOrCreateActive(1)
		if err != nil {
			writeExportError(w, http.StatusInternalServerError, "signing key unavailable")
			return
		}
		manifest = SignManifest(manifest, activeKey.KID, priv)

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)

		manifestLine, mErr := MarshalManifestLine(manifest)
		writeJSONLLine(w, manifestLine, mErr)
		for _, k := range keys {
			line, err := MarshalKeyLine(k)
			writeJSONLLine(w, line, err)
		}
		for _, rec := range receipts {
			line, err := MarshalJSONLReceipt(rec)
			writeJSONLLine(w, line, err)
		}
	}
}

// resolveExportRange is the pure decision logic behind range validation and
// clamping, split out from ExportHandler so it can be unit tested without a
// database. It clamps an absent or too-large "to" down to headSeq, then
// rejects (from beyond head+1) and (resolved range too large).
func resolveExportRange(from, requestedTo uint64, hasTo bool, headSeq uint64) (resolvedTo uint64, errMsg string, ok bool) {
	if from > headSeq+1 {
		return 0, "from is beyond the current head", false
	}
	resolvedTo = headSeq
	if hasTo && requestedTo < headSeq {
		resolvedTo = requestedTo
	}
	var rangeSize int64
	if resolvedTo >= from {
		rangeSize = int64(resolvedTo-from) + 1
	}
	if rangeSize > MaxExportRange {
		return 0, "range exceeds the maximum export size", false
	}
	return resolvedTo, "", true
}

func parseExportRange(w http.ResponseWriter, r *http.Request) (from, to uint64, hasTo, ok bool) {
	fromStr := r.URL.Query().Get("from")
	if fromStr == "" {
		writeExportError(w, http.StatusBadRequest, "from is required")
		return 0, 0, false, false
	}
	from, err := strconv.ParseUint(fromStr, 10, 64)
	if err != nil || from < 1 {
		writeExportError(w, http.StatusBadRequest, "from must be a positive integer")
		return 0, 0, false, false
	}

	toStr := r.URL.Query().Get("to")
	if toStr == "" {
		return from, 0, false, true
	}
	to, err = strconv.ParseUint(toStr, 10, 64)
	if err != nil || to < from {
		writeExportError(w, http.StatusBadRequest, "to must be an integer >= from")
		return 0, 0, false, false
	}
	return from, to, true, true
}

func writeExportError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSONLLine(w http.ResponseWriter, line []byte, err error) {
	if err != nil {
		return // the response is already committed by the time marshaling could fail; nothing safe left to do
	}
	w.Write(line)
	w.Write([]byte("\n"))
}

func queryEntryHashAt(ctx context.Context, tx *sql.Tx, seq uint64) ([32]byte, error) {
	var hash []byte
	row := tx.QueryRowContext(ctx, `SELECT entry_hash FROM receipts WHERE seq = ?`, seq)
	if err := row.Scan(&hash); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	if len(hash) != len(out) {
		return [32]byte{}, errors.New("receipt: anchor entry_hash has unexpected length")
	}
	copy(out[:], hash)
	return out, nil
}

func queryReceiptRange(ctx context.Context, tx *sql.Tx, from, to uint64) ([]Receipt, error) {
	if to < from {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+receiptColumns+` FROM receipts WHERE seq BETWEEN ? AND ? ORDER BY seq ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Receipt
	for rows.Next() {
		r, err := ScanReceiptRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func queryAllSignerKeys(ctx context.Context, tx *sql.Tx) ([]TrustedKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT kid, public_key, valid_from_seq, valid_until_seq FROM signer_keys ORDER BY valid_from_seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TrustedKey
	for rows.Next() {
		var k TrustedKey
		var pub []byte
		var validUntil sql.NullInt64
		if err := rows.Scan(&k.KID, &pub, &k.ValidFromSeq, &validUntil); err != nil {
			return nil, err
		}
		k.PublicKey = pub
		if validUntil.Valid {
			v := uint64(validUntil.Int64)
			k.ValidUntilSeq = &v
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
