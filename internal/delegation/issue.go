/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"time"

	biscuit "github.com/biscuit-auth/biscuit-go/v2"
	"github.com/biscuit-auth/biscuit-go/v2/parser"
)

// grantFactName is the single authority-block fact a verified Grant carries.
// Keeping every bound field in one fact (rather than several
// separately-satisfiable facts) makes it harder for an attacker-controlled
// attenuation block to synthesize a false match by combining unrelated
// facts; Verify's GetBlockID check additionally requires this exact fact to
// originate from block 0.
const grantFactName = "grant"

// Grant is the direct authority a human principal has given one agent to
// take one action on one service, expiring at a fixed time. It is not
// itself an attenuation: a token containing only its authority block (no
// appended blocks) is a "direct grant" (DELG-04).
type Grant struct {
	HumanPrincipal string
	AgentKeyID     string
	Service        string
	Action         string
	Expiry         time.Time
}

// Issue builds and serializes a single-block Biscuit encoding grant, signed
// by rootPriv. This is the direct-grant case: no attenuation blocks.
func Issue(rootPriv ed25519.PrivateKey, grant Grant) ([]byte, error) {
	fact := biscuit.Fact{Predicate: biscuit.Predicate{
		Name: grantFactName,
		IDs: []biscuit.Term{
			biscuit.String(grant.HumanPrincipal),
			biscuit.String(grant.AgentKeyID),
			biscuit.String(grant.Service),
			biscuit.String(grant.Action),
			biscuit.Date(grant.Expiry),
		},
	}}

	builder := biscuit.NewBuilder(rootPriv)
	if err := builder.AddAuthorityFact(fact); err != nil {
		return nil, fmt.Errorf("delegation: add authority fact: %w", err)
	}
	b, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("delegation: build: %w", err)
	}
	token, err := b.Serialize()
	if err != nil {
		return nil, fmt.Errorf("delegation: serialize: %w", err)
	}
	return token, nil
}

// Attenuate appends one restricting block to an existing token — the
// attenuation path a delegating agent adds when it hands narrower
// authority to a sub-agent. checkDatalog must contain only `check if`
// clauses: Biscuit blocks may only narrow, never grant new authority
// (that authority lives solely in the authority block's grant fact, and
// Verify's GetBlockID check rejects any grant fact found outside block 0).
func Attenuate(token []byte, checkDatalog string) ([]byte, error) {
	b, err := biscuit.Unmarshal(token)
	if err != nil {
		return nil, fmt.Errorf("delegation: unmarshal: %w", err)
	}
	block, err := parser.FromStringBlock(checkDatalog)
	if err != nil {
		return nil, fmt.Errorf("delegation: parse attenuation block: %w", err)
	}
	blockBuilder := b.CreateBlock()
	if err := blockBuilder.AddBlock(block); err != nil {
		return nil, fmt.Errorf("delegation: add block: %w", err)
	}
	attenuated, err := b.Append(rand.Reader, blockBuilder.Build())
	if err != nil {
		return nil, fmt.Errorf("delegation: append: %w", err)
	}
	out, err := attenuated.Serialize()
	if err != nil {
		return nil, fmt.Errorf("delegation: serialize: %w", err)
	}
	return out, nil
}
