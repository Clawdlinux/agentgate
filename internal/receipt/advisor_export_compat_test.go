package receipt

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

// frozenMainExport retains the v1 export assembly from origin/main at 494e81b.
func frozenMainExport(t *testing.T, l *Ledger, store *signer.Store) []byte {
	t.Helper()
	var output bytes.Buffer
	err := store.WithActiveKey(1, func(key signer.KeyRecord, priv ed25519.PrivateKey) error {
		tx, err := l.db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		head, hash, err := readHead(t.Context(), tx)
		if err != nil {
			return err
		}
		all, err := queryReceiptRange(t.Context(), tx, 1, head)
		if err != nil {
			return err
		}
		keys, err := queryAllSignerKeys(t.Context(), tx)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		manifest := ExportManifest{FormatVersion: 1, RequestedFrom: 1, ResolvedTo: head, Count: len(all),
			HeadSeq: head, HeadHash: hash, LastEntryHash: hash, KeysetDigest: ComputeKeysetDigest(keys)}
		if len(all) > 0 {
			manifest.FirstEntryHash = all[0].EntryHash
		}
		manifest = SignManifest(manifest, key.KID, priv)
		write := func(line []byte, err error) error {
			if err != nil {
				return err
			}
			output.Write(line)
			output.WriteByte('\n')
			return nil
		}
		if err := write(MarshalManifestLine(manifest)); err != nil {
			return err
		}
		for _, k := range keys {
			if err := write(MarshalKeyLine(k)); err != nil {
				return err
			}
		}
		for _, r := range all {
			if err := write(MarshalJSONLReceipt(r)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestDefaultExportRemainsFrozenV1(t *testing.T) {
	l, store := newTestLedgerAndStore(t)
	d := testDraft("github", "write")
	d.AdvisorRecord = json.RawMessage(`{"outcome":"allow"}`)
	if _, err := l.Append(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	ExportHandler(l.db, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?from=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), frozenMainExport(t, l, store)) {
		t.Fatal("default export changed from frozen main v1 assembly")
	}
	bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Records) != 0 {
		t.Fatal("default export contains record extensions")
	}
	result, err := receiptspec.VerifyChain(bundle.Receipts, bundle.Keys, receiptspec.Anchor{}, nil)
	if err != nil || !result.OK {
		t.Fatalf("legacy chain verification failed: %+v %v", result, err)
	}
}

func TestLegacyAdvisorNamesRemainOrdinary(t *testing.T) {
	l, store := newTestLedgerAndStore(t)
	legacy := testDraft("advisor", "decision:advise")
	legacy.PolicyDecision, legacy.StatusCode = "deny", 404
	if _, err := l.Append(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"from=1", "from=1&records=1"} {
		w := httptest.NewRecorder()
		ExportHandler(l.db, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?"+query, nil))
		if w.Code != 200 {
			t.Fatalf("old history became unexportable: %s", w.Body.String())
		}
		bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		result, err := receiptspec.VerifyBundle(bundle, nil, nil)
		if err != nil || !result.OK {
			t.Fatalf("old history failed verification: %+v %v", result, err)
		}
	}
}

func TestLeadingCompanionVerifiesAgainstManifestAnchor(t *testing.T) {
	l, store := newTestLedgerAndStore(t)
	d := testDraft("github", "write")
	d.AdvisorRecord = json.RawMessage(`{"outcome":"allow"}`)
	action, err := l.Append(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	ExportHandler(l.db, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?from=2&records=1", nil))
	if w.Code != 200 {
		t.Fatalf("leading companion rejected: %s", w.Body.String())
	}
	bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Receipts) != 1 || len(bundle.Records) != 1 || bundle.Manifest.AnchorSeq != action.Seq ||
		bundle.Manifest.AnchorHash != action.EntryHash {
		t.Fatal("missing action anchor")
	}
	result, err := receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || !result.OK {
		t.Fatalf("anchored companion failed: %+v %v", result, err)
	}
	bundle.Manifest = nil
	result, err = receiptspec.VerifyBundle(bundle, nil, nil)
	if err == nil && result.OK {
		t.Fatal("manifest-less leading companion passed")
	}
}
