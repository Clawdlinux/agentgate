/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"errors"
	"testing"
	"time"

	biscuit "github.com/biscuit-auth/biscuit-go/v2"
	"github.com/biscuit-auth/biscuit-go/v2/datalog"
	"github.com/biscuit-auth/biscuit-go/v2/parser"
)

func TestNewVerifier_Limits(t *testing.T) {
	t.Parallel()
	pub, _ := testKeypair(t)
	defaults := Limits{
		MaxDuration: 250 * time.Millisecond, MaxFacts: 1000, MaxIterations: 100,
	}
	if got := DefaultLimits(); got != defaults {
		t.Fatalf("DefaultLimits = %+v, want %+v", got, defaults)
	}
	custom := Limits{
		MaxDuration: 500 * time.Millisecond, MaxFacts: 20, MaxIterations: 3,
	}
	for _, tc := range []struct {
		name string
		opts []VerifierOption
		want Limits
	}{
		{"defaults", nil, defaults},
		{"custom", []VerifierOption{WithLimits(custom)}, custom},
		{"zero keeps defaults", []VerifierOption{WithLimits(Limits{})}, defaults},
		{"partial", []VerifierOption{WithLimits(Limits{MaxFacts: 2})},
			Limits{MaxDuration: defaults.MaxDuration, MaxFacts: 2, MaxIterations: defaults.MaxIterations}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewVerifier(pub, tc.opts...).limits; got != tc.want {
				t.Fatalf("limits = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVerify_LimitExceededDenied(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err = Attenuate(token, `seed(0); derived($n) <- seed($n); check if derived(0);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier(pub).Verify(token, "alice", "agent-1", "github", "list_repos", now); err != nil {
		t.Fatalf("default limits must allow the token: %v", err)
	}

	for _, tc := range []struct {
		name   string
		limits Limits
		cause  error
	}{
		{"authority facts", Limits{MaxFacts: 1}, datalog.ErrWorldRunLimitMaxFacts},
		{"attenuation facts", Limits{MaxFacts: 3}, datalog.ErrWorldRunLimitMaxFacts},
		{"attenuation iterations", Limits{MaxIterations: 1}, datalog.ErrWorldRunLimitMaxIterations},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain, err := NewVerifier(pub, WithLimits(tc.limits)).
				Verify(token, "alice", "agent-1", "github", "list_repos", now)
			if !errors.Is(err, ErrDenied) || !errors.Is(err, tc.cause) {
				t.Fatalf("Verify error = %v, want ErrDenied and %v", err, tc.cause)
			}
			if chain != nil {
				t.Fatalf("denied token returned lineage: %v", chain)
			}
		})
	}

	_, err = NewVerifier(pub).Verify(token, "mallory", "agent-1", "github", "list_repos", now)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("policy error = %v, want ErrDenied", err)
	}
	for _, cause := range []error{
		datalog.ErrWorldRunLimitMaxFacts, datalog.ErrWorldRunLimitMaxIterations, datalog.ErrWorldRunLimitTimeout,
	} {
		if errors.Is(err, cause) {
			t.Fatalf("policy denial misclassified as a limit: %v", err)
		}
	}
}

func TestVerifier_QueryRetainsLimitsAfterAuthorize(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	token, err := Issue(priv, Grant{
		HumanPrincipal: "alice", AgentKeyID: "agent-1", Service: "github", Action: "list_repos",
		Expiry: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := biscuit.Unmarshal(token)
	if err != nil {
		t.Fatal(err)
	}
	v := NewVerifier(pub, WithLimits(Limits{MaxFacts: 2}))
	authorizer, err := v.newAuthorizer(b)
	if err != nil {
		t.Fatal(err)
	}
	authorizer.AddPolicy(biscuit.DefaultAllowPolicy)
	if err := authorizer.Authorize(); err != nil {
		t.Fatalf("Authorize below limit: %v", err)
	}
	authorizer.AddFact(biscuit.Fact{Predicate: biscuit.Predicate{Name: "extra"}})
	rule, err := parser.FromStringRule(`matched() <- extra()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.Query(rule); !errors.Is(err, datalog.ErrWorldRunLimitMaxFacts) {
		t.Fatalf("Query error = %v, want the configured fact limit", err)
	}
}
