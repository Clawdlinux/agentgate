package receipt

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// EvidenceHandler serves a separate record. Mount it behind the same admin authentication as receipt exports.
func EvidenceHandler(ledger *Ledger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seq, err := strconv.ParseUint(r.PathValue("seq"), 10, 64)
		if err != nil || seq == 0 {
			http.Error(w, "invalid sequence", http.StatusBadRequest)
			return
		}
		record, err := ledger.Record(r.Context(), seq)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "evidence not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "evidence unavailable or binding invalid", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(record)
	}
}
