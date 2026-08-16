/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// publicKeyWire is the GET /v1/receipts/pubkey JSON shape for one key.
// It carries no private material by construction.
type publicKeyWire struct {
	KID           string  `json:"kid"`
	PublicKeyHex  string  `json:"public_key_hex"`
	ValidFromSeq  uint64  `json:"valid_from_seq"`
	ValidUntilSeq *uint64 `json:"valid_until_seq,omitempty"`
}

type pubkeyResponse struct {
	Keys []publicKeyWire `json:"keys"`
}

// PubkeyHandler serves GET /v1/receipts/pubkey: every active and historical
// public key, hex-encoded, with sequence validity intervals. Phase 4 mounts
// this into the running server's router.
func PubkeyHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		records, err := store.PublicKeys()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		response := pubkeyResponse{Keys: make([]publicKeyWire, 0, len(records))}
		for _, record := range records {
			response.Keys = append(response.Keys, publicKeyWire{
				KID:           record.KID,
				PublicKeyHex:  hex.EncodeToString(record.PublicKey),
				ValidFromSeq:  record.ValidFromSeq,
				ValidUntilSeq: record.ValidUntilSeq,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
