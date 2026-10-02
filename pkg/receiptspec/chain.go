/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import "time"

// Fields are the producer-supplied parts of a receipt. Chain fills in Seq,
// PrevHash, EntryHash, SignerKID, and Signature.
type Fields struct {
	TimestampUnixNS uint64 // 0 means time.Now()
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

// Chain is an in-memory producer of consecutive signed receipts. It is not
// safe for concurrent use and does not persist anything: store each returned
// receipt durably before acting on it, and recreate the Chain from the last
// stored (seq, entry_hash) after a restart.
type Chain struct {
	signer   Signer
	headSeq  uint64
	headHash [32]byte
}

// NewChain returns a Chain whose next receipt follows (prevSeq, prevHash).
// Use 0 and the zero hash to start at genesis.
func NewChain(s Signer, prevSeq uint64, prevHash [32]byte) *Chain {
	return &Chain{signer: s, headSeq: prevSeq, headHash: prevHash}
}

// Head returns the last appended (seq, entry_hash).
func (c *Chain) Head() (uint64, [32]byte) {
	return c.headSeq, c.headHash
}

// Next builds, validates, and signs the receipt after Head. The head only
// advances on success.
func (c *Chain) Next(f Fields) (Receipt, error) {
	ts := f.TimestampUnixNS
	if ts == 0 {
		ts = uint64(time.Now().UnixNano())
	}
	r, err := Seal(Receipt{
		Seq:             c.headSeq + 1,
		TimestampUnixNS: ts,
		HumanPrincipal:  f.HumanPrincipal,
		AgentKeyID:      f.AgentKeyID,
		DelegationChain: f.DelegationChain,
		Service:         f.Service,
		Action:          f.Action,
		ParamsSHA256:    f.ParamsSHA256,
		PolicyDecision:  f.PolicyDecision,
		StatusCode:      f.StatusCode,
		LatencyMS:       f.LatencyMS,
		Error:           f.Error,
		PrevHash:        c.headHash,
	}, c.signer)
	if err != nil {
		return Receipt{}, err
	}
	c.headSeq, c.headHash = r.Seq, r.EntryHash
	return r, nil
}
