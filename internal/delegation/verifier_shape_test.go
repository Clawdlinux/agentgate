/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"errors"
	"strings"
	"testing"
	"time"

	biscuit "github.com/biscuit-auth/biscuit-go/v2"
	"github.com/biscuit-auth/biscuit-go/v2/parser"
)

func TestVerify_TokenShapeRejectedBeforeAuthorizer(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const grant = `grant("alice", "agent-1", "github", "list_repos", 2026-10-02T00:00:00Z);`
	rootToken := func(code string) []byte {
		t.Helper()
		block, err := parser.FromStringBlock(code)
		if err != nil {
			t.Fatal(err)
		}
		builder := biscuit.NewBuilder(priv)
		if err := builder.AddBlock(block); err != nil {
			t.Fatal(err)
		}
		b, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		token, err := b.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token := rootToken(grant)
	attenuated := func(code string, count int) []byte {
		t.Helper()
		out := token
		for range count {
			var err error
			out, err = Attenuate(out, code)
			if err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	secret := "private-token-content"
	oversized := rootToken(strings.Replace(grant, `"github"`, `"`+strings.Repeat(secret, 250)+`"`, 1))
	bomb := `check if f($a), f($b), f($c), f($d), f($e);`
	regex := `check if "a".matches("a");`
	overlongExpression := `check if ` + strings.Repeat("1 + ", 15) + `1 == 16;`
	parsed, err := parser.FromStringBlock(overlongExpression)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(parsed.Checks[0].Queries[0].Expressions[0]); got != 33 {
		t.Fatalf("oversized expression fixture has %d ops, want 33", got)
	}
	tooManyExpressions := `check if ` + strings.TrimSuffix(strings.Repeat("true, ", 9), ", ") + `;`
	for _, tc := range []struct {
		name   string
		token  []byte
		limits Limits
	}{
		{"oversized valid token", oversized, Limits{}},
		{"oversized malformed token", []byte(strings.Repeat("x", 4097)), Limits{}},
		{"too many blocks", attenuated(`check if true;`, 9), Limits{}},
		{"attenuation fact", attenuated(`f(0);`, 1), Limits{}},
		{"attenuation rule", attenuated(`f($a) <- f($a);`, 1), Limits{}},
		{"attenuation predicates", attenuated(bomb, 1), Limits{}},
		{"attenuation later query predicates", attenuated(strings.Replace(bomb, "check if ", "check if true or ", 1), 1), Limits{}},
		{"fact and predicate bomb", attenuated(`f(0); f(1); check if f($a), f($b), f($c);`, 1), Limits{}},
		{"attenuation checks", attenuated(strings.Repeat(`check if true;`, 17), 1), Limits{}},
		{"attenuation regex", attenuated(regex, 1), Limits{}},
		{"attenuation later regex expression", attenuated(`check if true, "a".matches("a");`, 1), Limits{}},
		{"attenuation later regex query", attenuated(`check if true or "a".matches("a");`, 1), Limits{}},
		{"attenuation expression ops", attenuated(overlongExpression, 1), Limits{}},
		{"attenuation expressions", attenuated(tooManyExpressions, 1), Limits{}},
		{"authority checks", rootToken(grant + strings.Repeat(`check if true;`, 17)), Limits{}},
		{"authority regex", rootToken(grant + regex), Limits{}},
		{"authority expression ops", rootToken(grant + overlongExpression), Limits{}},
		{"authority expressions", rootToken(grant + tooManyExpressions), Limits{}},
		{"authority predicates", rootToken(grant + bomb), Limits{}},
		{"authority rule", rootToken(grant + `f($a) <- f($a);`), Limits{}},
		{"authority extra fact", rootToken(grant + `f(0);`), Limits{}},
		{"authority multiple grants", rootToken(grant + strings.Replace(grant, `"alice"`, `"bob"`, 1)), Limits{}},
		{"authority missing grant", rootToken(`f(0);`), Limits{}},
		{"authority missing fact", rootToken(`check if true;`), Limits{}},
		{"authority wrong grant arity", rootToken(`grant("alice");`), Limits{}},
		{"authority wrong grant type", rootToken(`grant("alice", "agent-1", "github", "list_repos", 1);`), Limits{}},
		{"custom byte cap", token, Limits{MaxTokenBytes: len(token) - 1}},
		{"custom block cap", attenuated(`check if true;`, 2), Limits{MaxBlocks: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVerifier(pub, WithLimits(tc.limits))
			calls := 0
			v.authorizerFactory = func(*biscuit.Biscuit) (biscuit.Authorizer, error) {
				calls++
				return nil, errors.New("authorizer must not be built")
			}
			chain, err := v.Verify(tc.token, "alice", "agent-1", "github", "list_repos", now)
			if chain != nil || calls != 0 {
				t.Fatalf("chain = %v, authorizer calls = %d, want nil and 0", chain, calls)
			}
			if !errors.Is(err, ErrDenied) || !errors.Is(err, ErrTokenShape) {
				t.Fatalf("Verify error = %v, want ErrDenied and ErrTokenShape", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "alice") {
				t.Fatalf("shape error contains token content: %v", err)
			}
		})
	}
}

func TestVerify_TokenShapeBoundariesAllowed(t *testing.T) {
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

	predicate := `grant($p, $a, $s, $act, $exp)`
	maxOps := `check if (` + strings.Repeat("1 + ", 14) + `1 == 15);`
	parsed, err := parser.FromStringBlock(maxOps)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(parsed.Checks[0].Queries[0].Expressions[0]); got != 32 {
		t.Fatalf("boundary expression fixture has %d ops, want 32", got)
	}
	for _, tc := range []struct {
		name  string
		code  string
		count int
	}{
		{"max blocks", `check if true;`, 8},
		{"max checks", strings.Repeat(`check if true;`, 16), 1},
		{"max predicates", `check if ` + strings.TrimSuffix(strings.Repeat(predicate+", ", 4), ", ") + `;`, 1},
		{"max expression ops", maxOps, 1},
		{"max expressions", `check if ` + strings.TrimSuffix(strings.Repeat("true, ", 8), ", ") + `;`, 1},
		{"date and equality", `check if ` + predicate + `, $exp >= 2026-10-01T00:00:00Z, $p == "alice";`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := token
			for range tc.count {
				var err error
				out, err = Attenuate(out, tc.code)
				if err != nil {
					t.Fatal(err)
				}
			}
			v := NewVerifier(pub, WithLimits(Limits{MaxTokenBytes: len(out)}))
			calls := 0
			v.authorizerFactory = func(b *biscuit.Biscuit) (biscuit.Authorizer, error) {
				calls++
				return v.newAuthorizer(b)
			}
			chain, err := v.Verify(out, "alice", "agent-1", "github", "list_repos", now)
			if err != nil || len(chain) != tc.count || calls != 1 {
				t.Fatalf("chain = %v, error = %v, authorizer calls = %d", chain, err, calls)
			}
		})
	}
}

func TestVerify_AuthorityChecksAllowed(t *testing.T) {
	t.Parallel()
	pub, priv := testKeypair(t)
	block, err := parser.FromStringBlock(
		`grant("alice", "agent-1", "github", "list_repos", 2026-10-02T00:00:00Z);` +
			strings.Repeat(`check if true;`, 16),
	)
	if err != nil {
		t.Fatal(err)
	}
	builder := biscuit.NewBuilder(priv)
	if err := builder.AddBlock(block); err != nil {
		t.Fatal(err)
	}
	b, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	token, err := b.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	chain, err := NewVerifier(pub).Verify(token, "alice", "agent-1", "github", "list_repos", now)
	if err != nil || chain != nil {
		t.Fatalf("authority checks: chain = %v, error = %v", chain, err)
	}
}
