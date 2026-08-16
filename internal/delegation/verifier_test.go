/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func TestVerify_DirectGrantPasses(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	chain, err := v.Verify(token, "alice", "agent-1", "github", "list_repos", time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if chain != nil {
		t.Fatalf("chain = %v, want nil for a direct grant (DELG-04)", chain)
	}
}

// TestVerify_MismatchedContextDenied covers DELG-02: every bound field
// must match, not just some of them.
func TestVerify_MismatchedContextDenied(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	cases := []struct {
		name                              string
		principal, agent, service, action string
	}{
		{"wrong principal", "mallory", "agent-1", "github", "list_repos"},
		{"wrong agent", "alice", "agent-2", "github", "list_repos"},
		{"wrong service", "alice", "agent-1", "slack", "list_repos"},
		{"wrong action", "alice", "agent-1", "github", "delete_repo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := v.Verify(token, c.principal, c.agent, c.service, c.action, time.Now()); err == nil {
				t.Fatal("expected a mismatched context to be denied")
			}
		})
	}
}

func TestVerify_ExpiredDenied(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := v.Verify(token, "alice", "agent-1", "github", "list_repos", time.Now()); err == nil {
		t.Fatal("expected an expired grant to be denied")
	}
}

func TestVerify_WrongRootKeyDenied(t *testing.T) {
	t.Parallel()
	_, priv := testKeypair(t)
	otherPub, _ := testKeypair(t)
	v := NewVerifier(otherPub) // verifier trusts a DIFFERENT root than the issuer used

	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := v.Verify(token, "alice", "agent-1", "github", "list_repos", time.Now()); err == nil {
		t.Fatal("expected a token signed by an untrusted root to be denied")
	}
}

// TestVerify_AttenuatedGrantPreservesOrderedLineage covers DELG-03 and
// DELG-04: an attenuated token's delegation_chain is non-empty, ordered,
// and contains only digests, never the block's own datalog source text.
func TestVerify_AttenuatedGrantPreservesOrderedLineage(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	once, err := Attenuate(token, `check if true;`)
	if err != nil {
		t.Fatalf("Attenuate (1st): %v", err)
	}
	twice, err := Attenuate(once, `check if false or true;`)
	if err != nil {
		t.Fatalf("Attenuate (2nd): %v", err)
	}

	chain, err := v.Verify(twice, "alice", "agent-1", "github", "list_repos", time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("chain length = %d, want 2: %v", len(chain), chain)
	}
	for _, element := range chain {
		if len(element) != 64 { // hex-encoded SHA-256
			t.Fatalf("chain element %q is not a 64-char hex digest", element)
		}
		for _, c := range element {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Fatalf("chain element %q contains non-hex characters, may be raw source text", element)
			}
		}
	}
	if chain[0] == chain[1] {
		t.Fatal("two differently-worded attenuation blocks produced identical digests")
	}
}

// TestVerify_SplicedTokenRejectedForWrongContext covers DELG-05: a fully
// valid, unmodified token issued for one context cannot be reused,
// unmodified, to authorize a different context it was never granted.
func TestVerify_SplicedTokenRejectedForWrongContext(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	chainA, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-A", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue chain A: %v", err)
	}

	// chain A verifies fine in its own, correct context.
	if _, err := v.Verify(chainA, "alice", "agent-A", "github", "list_repos", time.Now()); err != nil {
		t.Fatalf("chain A should verify in its own context: %v", err)
	}

	// The attacker presents chain A's real, unmodified, validly-signed
	// bytes for chain B's intended context (a different agent, service,
	// and action never granted to chain A).
	if _, err := v.Verify(chainA, "alice", "agent-B", "slack", "post_message", time.Now()); err == nil {
		t.Fatal("expected chain A's grant to be rejected when presented for chain B's context")
	}
}

// TestVerify_WideningViaAppendedFakeGrantFactRejected covers DELG-05's
// deeper case: a token holder can freely attenuate (append blocks to) a
// token they legitimately hold, but appending a block containing a new,
// wider grant(...) fact must not widen the token's authority — the
// authority is bound to whatever the root actually signed into block 0.
func TestVerify_WideningViaAppendedFakeGrantFactRejected(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	v := NewVerifier(pub)

	chainB, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-B", Service: "slack", Action: "post_message",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue chain B: %v", err)
	}

	widened, err := Attenuate(chainB, `grant("alice", "agent-B", "github", "list_repos", 2030-01-01T00:00:00Z);`)
	if err != nil {
		t.Fatalf("Attenuate (inject wider grant fact): %v", err)
	}

	if _, err := v.Verify(widened, "alice", "agent-B", "github", "list_repos", time.Now()); err == nil {
		t.Fatal("expected a holder-appended grant fact to be rejected, not treated as authoritative")
	}

	// The original, unwidened scope must still work.
	if _, err := v.Verify(widened, "alice", "agent-B", "slack", "post_message", time.Now()); err != nil {
		t.Fatalf("the token's real, root-granted scope should still verify: %v", err)
	}
}

func TestVerify_MalformedTokenDenied(t *testing.T) {
	t.Parallel()
	pub, _ := testKeypair(t)
	v := NewVerifier(pub)

	if _, err := v.Verify([]byte("not a biscuit token"), "alice", "agent-1", "github", "list_repos", time.Now()); err == nil {
		t.Fatal("expected a malformed token to be denied")
	}
}
