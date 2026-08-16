/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	biscuit "github.com/biscuit-auth/biscuit-go/v2"
	"github.com/biscuit-auth/biscuit-go/v2/parser"
)

// ErrDenied means the token failed cryptographic verification or its
// bound grant does not match the request it accompanies (DELG-01,
// DELG-02). It is always a hard deny: the caller must not proceed to
// registry, vault, or upstream access.
var ErrDenied = errors.New("delegation: denied")

// Verifier checks a presented Biscuit token against one root public key.
type Verifier struct {
	rootPublicKey ed25519.PublicKey
}

// NewVerifier returns a Verifier trusting exactly one root public key.
func NewVerifier(rootPublicKey ed25519.PublicKey) *Verifier {
	return &Verifier{rootPublicKey: rootPublicKey}
}

// Verify checks token's signature chain against the trusted root, then
// requires its authority grant to match humanPrincipal, agentKeyID,
// service, and action exactly, and to not be expired as of now (DELG-02).
//
// It returns the ordered, privacy-safe attenuation-path commitments to
// store in a receipt's delegation_chain: one SHA-256 digest (hex-encoded)
// per appended block, in append order, covering the block's canonical
// datalog content — never the raw token bytes or the block's own source
// text (DELG-03). A token with only its authority block (no attenuation)
// returns a nil chain: a direct grant (DELG-04).
//
// A block appended by whoever is holding the token (not the root) can
// itself carry facts, including a same-named grant(...) fact — Biscuit's
// authorization world sees every block's facts together by default. Verify
// closes that gap explicitly: after the Datalog policy matches, it
// requires the grant fact that satisfied it to originate from block 0
// (the authority block signed by root), via Biscuit.GetBlockID. A grant
// fact injected by any later, holder-appended block is rejected even if it
// would otherwise satisfy the policy — this is what defeats an attacker
// splicing a wider or differently-scoped grant claim into a token they
// hold (DELG-05).
func (v *Verifier) Verify(token []byte, humanPrincipal, agentKeyID, service, action string, now time.Time) ([]string, error) {
	b, err := biscuit.Unmarshal(token)
	if err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %v", ErrDenied, err)
	}

	authorizer, err := b.Authorizer(v.rootPublicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: signature chain: %v", ErrDenied, err)
	}

	params := map[string]biscuit.Term{
		"p":   biscuit.String(humanPrincipal),
		"a":   biscuit.String(agentKeyID),
		"s":   biscuit.String(service),
		"act": biscuit.String(action),
		"now": biscuit.Date(now),
	}
	parsedAuthorizer, err := parser.FromStringAuthorizerWithParams(
		`allow if `+grantFactName+`({p}, {a}, {s}, {act}, $exp), {now} <= $exp;`,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("delegation: parse authorizer: %w", err)
	}
	authorizer.AddAuthorizer(parsedAuthorizer)

	if err := authorizer.Authorize(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDenied, err)
	}

	matchRule, err := parser.FromStringRuleWithParams(
		grantFactName+`({p}, {a}, {s}, {act}, $exp) <- `+grantFactName+`({p}, {a}, {s}, {act}, $exp)`,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("delegation: parse match rule: %w", err)
	}
	matched, err := authorizer.Query(matchRule)
	if err != nil {
		return nil, fmt.Errorf("delegation: query matched grant: %w", err)
	}
	if len(matched) == 0 {
		// Authorize() passed but the grant fact cannot be re-located —
		// treat as denied rather than silently accepting (defensive; this
		// should be unreachable given a consistent Datalog world).
		return nil, fmt.Errorf("%w: matched grant fact not found", ErrDenied)
	}
	for _, fact := range matched {
		blockID, err := b.GetBlockID(fact)
		if err != nil {
			return nil, fmt.Errorf("%w: grant fact not attributable to any block: %v", ErrDenied, err)
		}
		if blockID != 0 {
			return nil, fmt.Errorf("%w: grant fact originates outside the authority block (block %d)", ErrDenied, blockID)
		}
	}

	return attenuationChain(b), nil
}

// attenuationChain returns one SHA-256 digest per attenuation block, in
// append order — the ordered commitments DELG-03 and DELG-04 require.
// Biscuit.BlockCount and Biscuit.Code both count and index only the
// blocks appended after authority; a single-block (authority-only) token
// has BlockCount() == 0 and returns nil here.
func attenuationChain(b *biscuit.Biscuit) []string {
	count := b.BlockCount()
	if count == 0 {
		return nil
	}
	code := b.Code()
	chain := make([]string, 0, count)
	for i := 0; i < count && i < len(code); i++ {
		sum := sha256.Sum256([]byte(code[i]))
		chain = append(chain, hex.EncodeToString(sum[:]))
	}
	return chain
}
