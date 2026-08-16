/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/delegation"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// delegationTestSetup mirrors testSetup but wires a real
// *delegation.Verifier, an upstream call counter (to prove a denied
// delegation never reaches the upstream), and a vault token so an
// allowed, delegated request can complete end to end.
func delegationTestSetup(t *testing.T) (srv *Server, rootPriv ed25519.PrivateKey, receipts *fakeReceipts, upstreamCalls *int) {
	t.Helper()

	upstreamCalls = new(int)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*upstreamCalls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)

	reg := registry.New()
	if err := reg.LoadBytes([]byte(`
services:
  github:
    base_url: ` + upstream.URL + `
    auth:
      type: oauth2
    actions:
      list_repos:
        method: GET
        path: /user/repos
`)); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(key)
	if err := store.Put("alice", "github", vault.Token{
		AccessToken: "user-github-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("vault.Put: %v", err)
	}

	rootPub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}

	receipts = &fakeReceipts{}
	srv = New(Config{
		Registry: reg,
		Vault:    store,
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"test-key": {ID: "agent-1", AllowedServices: []string{"*"}, AllowedUsers: []string{"*"}},
		}},
		Receipts:   receipts,
		Delegation: delegation.NewVerifier(rootPub),
	})
	return srv, priv, receipts, upstreamCalls
}

func delegationHeaderValue(t *testing.T, token []byte) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(token)
}

// TestAct_NoDelegationHeader_ChainEmptyAndUnaffected covers DELG-04: a
// request with no delegation token behaves exactly as it did before
// Phase 8, even with a Delegation verifier configured.
func TestAct_NoDelegationHeader_ChainEmptyAndUnaffected(t *testing.T) {
	t.Parallel()
	srv, _, receipts, upstreamCalls := delegationTestSetup(t)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"alice"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, body = %s", w.Code, b)
	}
	if *upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", *upstreamCalls)
	}
	if got := receipts.last().DelegationChain; got != nil {
		t.Fatalf("DelegationChain = %v, want nil (direct, unmediated request)", got)
	}
}

// TestAct_ValidDelegation_ChainRecordedAndRequestProceeds covers DELG-01
// through DELG-04 together: a verified direct-grant token reaches the
// upstream and its (empty) chain is recorded.
func TestAct_ValidDelegation_ChainRecordedAndRequestProceeds(t *testing.T) {
	t.Parallel()
	srv, rootPriv, receipts, upstreamCalls := delegationTestSetup(t)

	token, err := delegation.Issue(rootPriv, delegation.Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	body := `{"service":"github","action":"list_repos","on_behalf_of":"alice"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Agentgate-Delegation", delegationHeaderValue(t, token))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, body = %s", w.Code, b)
	}
	if *upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", *upstreamCalls)
	}
	draft := receipts.last()
	if draft.PolicyDecision != "allow" {
		t.Fatalf("PolicyDecision = %s, want allow", draft.PolicyDecision)
	}
	if draft.DelegationChain != nil {
		t.Fatalf("DelegationChain = %v, want nil for a direct grant", draft.DelegationChain)
	}
}

// TestAct_MismatchedDelegation_DeniedBeforeUpstreamButStillReceipted
// covers DELG-01, DELG-02, and DELG-05: a token valid for a different
// context is rejected before any registry, vault, or upstream access, and
// the denial itself is still receipted evidence.
func TestAct_MismatchedDelegation_DeniedBeforeUpstreamButStillReceipted(t *testing.T) {
	t.Parallel()
	srv, rootPriv, receipts, upstreamCalls := delegationTestSetup(t)

	// Issued for a different agent and action entirely.
	token, err := delegation.Issue(rootPriv, delegation.Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-2", Service: "slack", Action: "post_message",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	body := `{"service":"github","action":"list_repos","on_behalf_of":"alice"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Agentgate-Delegation", delegationHeaderValue(t, token))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, want 403, body = %s", w.Code, b)
	}
	if *upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0 \u2014 a denied delegation must never reach upstream", *upstreamCalls)
	}
	if receipts.count() != 1 {
		t.Fatalf("receipts = %d, want 1 \u2014 the denial itself must still be receipted", receipts.count())
	}
	draft := receipts.last()
	if draft.PolicyDecision != "deny" {
		t.Fatalf("PolicyDecision = %s, want deny", draft.PolicyDecision)
	}
	if draft.Error != "delegation_denied" {
		t.Fatalf("Error = %s, want delegation_denied", draft.Error)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != "delegation_denied" {
		t.Fatalf("response code = %s, want delegation_denied", resp.Code)
	}
}

// TestAct_DelegationHeaderWithoutConfiguredVerifier_FailsClosed covers the
// gateway's fail-closed default: a claimed delegation with no configured
// verifier is denied, never silently accepted.
func TestAct_DelegationHeaderWithoutConfiguredVerifier_FailsClosed(t *testing.T) {
	t.Parallel()
	srv, _, receipts := testSetup(t) // no Delegation configured

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Agentgate-Delegation", "any-value-at-all")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, want 403, body = %s", w.Code, b)
	}
	draft := receipts.last()
	if draft.PolicyDecision != "deny" || draft.Error != "delegation_denied" {
		t.Fatalf("draft = %+v, want a receipted delegation_denied deny", draft)
	}
}
