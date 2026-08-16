/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExportHandler_FullRangeVerifiesOffline(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	appendN(t, ledger, 5)

	req := httptest.NewRequest("GET", "/v1/receipts/export?from=1", nil)
	w := httptest.NewRecorder()
	ExportHandler(ledger.db, store)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("Content-Type = %s, want application/x-ndjson", ct)
	}

	receipts, keys, manifest := parseExportBody(t, w.Body.String())
	if manifest == nil {
		t.Fatal("no manifest line in export")
	}
	if manifest.Count != 5 || manifest.ResolvedTo != 5 || manifest.AnchorSeq != 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.ResolvedTo != manifest.HeadSeq {
		t.Fatal("a full export should reach the true head")
	}
	if err := VerifyManifest(*manifest, keys, keys); err != nil {
		t.Fatalf("VerifyManifest: %v", err)
	}

	result, err := VerifyChain(receipts, keys, Anchor{Seq: manifest.AnchorSeq, EntryHash: manifest.AnchorHash},
		&ExpectedHead{Seq: manifest.ResolvedTo, EntryHash: manifest.LastEntryHash})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK || !result.Complete {
		t.Fatalf("OK=%v Complete=%v, want both true", result.OK, result.Complete)
	}
}

func TestExportHandler_PartialRangeUsesRealAnchor(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	all := appendN(t, ledger, 6)

	req := httptest.NewRequest("GET", "/v1/receipts/export?from=3&to=5", nil)
	w := httptest.NewRecorder()
	ExportHandler(ledger.db, store)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	receipts, keys, manifest := parseExportBody(t, w.Body.String())
	if manifest.Count != 3 || manifest.ResolvedTo != 5 || manifest.AnchorSeq != 2 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.AnchorHash != all[1].EntryHash {
		t.Fatal("anchor hash does not match the real seq=2 receipt's entry hash")
	}
	if manifest.ResolvedTo == manifest.HeadSeq {
		t.Fatal("this is a partial export; it should not claim to reach the true head")
	}

	result, err := VerifyChain(receipts, keys, Anchor{Seq: manifest.AnchorSeq, EntryHash: manifest.AnchorHash},
		&ExpectedHead{Seq: manifest.ResolvedTo, EntryHash: manifest.LastEntryHash})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK || !result.Complete {
		t.Fatalf("OK=%v Complete=%v, want both true for a correctly anchored partial range", result.OK, result.Complete)
	}
}

func TestExportHandler_ToBeyondHeadIsClamped(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	appendN(t, ledger, 2)

	req := httptest.NewRequest("GET", "/v1/receipts/export?from=1&to=1000", nil)
	w := httptest.NewRecorder()
	ExportHandler(ledger.db, store)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	_, _, manifest := parseExportBody(t, w.Body.String())
	if manifest.RequestedTo != 1000 || manifest.ResolvedTo != 2 {
		t.Fatalf("manifest = %+v, want requested_to=1000 resolved_to=2", manifest)
	}
}

func TestExportHandler_InvalidRangesReject400(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	appendN(t, ledger, 3)
	db := ledger.db

	cases := []string{
		"/v1/receipts/export",             // missing from
		"/v1/receipts/export?from=0",      // from < 1
		"/v1/receipts/export?from=2&to=1", // to < from
		"/v1/receipts/export?from=100",    // beyond head+1
	}
	for _, url := range cases {
		req := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		ExportHandler(db, store)(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", url, w.Code)
		}
	}
}

func TestResolveExportRange_OversizedRangeRejected(t *testing.T) {
	t.Parallel()
	_, errMsg, ok := resolveExportRange(1, 20000, true, 20000)
	if ok {
		t.Fatal("expected a 20000-row range to be rejected")
	}
	if errMsg == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestResolveExportRange_ClampsAbsentOrOversizedTo(t *testing.T) {
	t.Parallel()
	// No "to" at all: clamp to head.
	resolvedTo, _, ok := resolveExportRange(1, 0, false, 5)
	if !ok || resolvedTo != 5 {
		t.Fatalf("resolvedTo = %d, ok = %v, want 5, true", resolvedTo, ok)
	}
	// "to" beyond head: clamp to head.
	resolvedTo, _, ok = resolveExportRange(1, 1000, true, 5)
	if !ok || resolvedTo != 5 {
		t.Fatalf("resolvedTo = %d, ok = %v, want 5, true", resolvedTo, ok)
	}
	// "to" within head: keep as requested.
	resolvedTo, _, ok = resolveExportRange(1, 3, true, 5)
	if !ok || resolvedTo != 3 {
		t.Fatalf("resolvedTo = %d, ok = %v, want 3, true", resolvedTo, ok)
	}
}

func TestResolveExportRange_FromBeyondHeadRejected(t *testing.T) {
	t.Parallel()
	_, _, ok := resolveExportRange(7, 0, false, 5)
	if ok {
		t.Fatal("expected from=7 with head=5 to be rejected")
	}
	// from == head+1 is the boundary case for an empty-but-valid export.
	if _, _, ok := resolveExportRange(6, 0, false, 5); !ok {
		t.Fatal("expected from == head+1 to be accepted (empty range)")
	}
}

// TestExportHandler_SmallValidRangeNotAffectedByClamping is a wiring sanity
// check: the size-cap boundary itself is exercised directly and cheaply by
// TestResolveExportRange_OversizedRangeRejected (no need for 10,000+ real
// rows). This confirms the handler still returns 200 for an ordinary small
// range once that pure decision function is wired in.
func TestExportHandler_SmallValidRangeNotAffectedByClamping(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	appendN(t, ledger, 3)

	req := httptest.NewRequest("GET", "/v1/receipts/export?from=1&to=2", nil)
	w := httptest.NewRecorder()
	ExportHandler(ledger.db, store)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a small valid range", w.Code)
	}
}

// TestExportHandler_NoRawParamsOrSensitiveData covers EXPT-05: the export
// carries only the receipt protocol's own fields, never raw parameters.
func TestExportHandler_NoRawParamsOrSensitiveData(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	const secret = "super-secret-oauth-token-do-not-leak"
	_, err := ledger.Append(t.Context(), Draft{
		HumanPrincipal: "user-1",
		AgentKeyID:     "agent-1",
		Service:        "github",
		Action:         "list_repos",
		PolicyDecision: "allow",
		StatusCode:     200,
		LatencyMS:      5,
		Error:          secret, // deliberately misusing the field to prove it isn't silently widened
	})
	// The Error field only accepts the validated stable-code charset,
	// so this Append is expected to fail closed rather than store it.
	if err == nil {
		t.Fatal("expected Append to reject a non-stable-code Error value")
	}

	appendN(t, ledger, 2)
	req := httptest.NewRequest("GET", "/v1/receipts/export?from=1", nil)
	w := httptest.NewRecorder()
	ExportHandler(ledger.db, store)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("export body contains a raw secret value")
	}
}

func parseExportBody(t *testing.T, body string) (receipts []Receipt, keys []TrustedKey, manifest *ExportManifest) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		switch DetectJSONLLineType([]byte(line)) {
		case "manifest":
			m, err := ParseManifestLine([]byte(line))
			if err != nil {
				t.Fatalf("ParseManifestLine: %v", err)
			}
			manifest = &m
		case "key":
			k, err := ParseKeyLine([]byte(line))
			if err != nil {
				t.Fatalf("ParseKeyLine: %v", err)
			}
			keys = append(keys, k)
		default:
			r, err := ParseJSONLReceipt([]byte(line))
			if err != nil {
				t.Fatalf("ParseJSONLReceipt: %v", err)
			}
			receipts = append(receipts, r)
		}
	}
	return receipts, keys, manifest
}
