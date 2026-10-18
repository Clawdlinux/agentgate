package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	"github.com/Clawdlinux/agentgate/internal/advisor"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

type advisorFunc func(context.Context, advisor.State, advisor.Questions) (advisor.Answers, error)

func (f advisorFunc) Decide(ctx context.Context, s advisor.State, q advisor.Questions) (advisor.Answers, error) {
	return f(ctx, s, q)
}

func advisorResponse(risk float64) advisor.Answers {
	return advisor.Answers{CascadeThreshold: .8, Attempts: []advisor.TierAnswer{{
		Tier: "A", ModelID: "test", Revision: strings.Repeat("a", 40), WeightsSHA256: strings.Repeat("b", 64),
		Probabilities: map[string]float64{"task_consistent": .99, "personal_data": .01, "risk": risk}, Confidence: .99,
	}}}
}

func advisorSetup(t *testing.T, risky bool) (*Server, *fakeReceipts, *atomic.Int32) {
	t.Helper()
	s, v, r := testSetup(t)
	calls := &atomic.Int32{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(up.Close)
	reg := registry.New()
	flag := "false"
	if risky {
		flag = "true"
	}
	if err := reg.LoadBytes([]byte("services:\n  github:\n    base_url: " + up.URL + "\n    auth:\n      type: bearer\n    actions:\n      write:\n        method: POST\n        path: /write\n        risky: " + flag + "\n")); err != nil {
		t.Fatal(err)
	}
	s.cfg.Registry = reg
	if err := v.Put("user-1", "github", vault.Token{AccessToken: "test"}); err != nil {
		t.Fatal(err)
	}
	return s, r, calls
}

func advisorRequest(s *Server) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(`{"service":"github","action":"write","on_behalf_of":"user-1","params":{"message":"hello"}}`))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestGatewayAdvisorEscalation(t *testing.T) {
	for _, tc := range []struct {
		risk              float64
		status            int
		decision, outcome string
		upstream          int32
	}{
		{.1, 200, "allow", "allow", 1}, {.7, 409, "deny", "require_approval", 0}, {.95, 403, "deny", "deny_by_advisor", 0},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			s, r, up := advisorSetup(t, true)
			s.cfg.AdvisorPolicy = advisor.DefaultPolicy()
			s.cfg.Advisor = advisorFunc(func(context.Context, advisor.State, advisor.Questions) (advisor.Answers, error) {
				return advisorResponse(tc.risk), nil
			})
			w := advisorRequest(s)
			if w.Code != tc.status || up.Load() != tc.upstream {
				t.Fatalf("status=%d upstream=%d", w.Code, up.Load())
			}
			d := r.last()
			if d.PolicyDecision != tc.decision {
				t.Fatal(d.PolicyDecision)
			}
			if err := receiptspec.VerifyRecordBinding(receipt.Receipt{ParamsSHA256: d.ParamsSHA256}, d.Record); err != nil {
				t.Fatal(err)
			}
			var e advisor.Evidence
			if err := json.Unmarshal(d.Record, &e); err != nil {
				t.Fatal(err)
			}
			if e.Outcome != tc.outcome || len(e.Attempts) != 1 || e.InputStateSHA256 == "" {
				t.Fatalf("evidence=%+v", e)
			}
			if tc.decision == "deny" && d.Error != "advisor_escalated" {
				t.Fatal(d.Error)
			}
		})
	}
}

func TestGatewayAdvisorTimeoutModes(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		status   int
		upstream int32
	}{
		{"allow_with_flag", 200, 1}, {"require_approval", 409, 0}, {"deny", 403, 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s, r, up := advisorSetup(t, true)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
			}))
			defer remote.Close()
			client, err := advisor.NewHTTPAdvisor(remote.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			s.cfg.Advisor = client
			s.cfg.AdvisorPolicy = advisor.DefaultPolicy()
			s.cfg.AdvisorPolicy.Unavailable = tc.mode
			start := time.Now()
			w := advisorRequest(s)
			if time.Since(start) > 300*time.Millisecond {
				t.Fatal("unbounded timeout")
			}
			if w.Code != tc.status || up.Load() != tc.upstream {
				t.Fatalf("status=%d upstream=%d", w.Code, up.Load())
			}
			d := r.last()
			var e advisor.Evidence
			if err := json.Unmarshal(d.Record, &e); err != nil {
				t.Fatal(err)
			}
			if !e.Unavailable || d.Error != "advisor_unavailable" {
				t.Fatal("missing unavailable evidence")
			}
		})
	}
}

func TestGatewayAdvisorCannotUndoDenyProperty(t *testing.T) {
	s, r, up := advisorSetup(t, true)
	var calls atomic.Int32
	s.cfg.Advisor = advisorFunc(func(context.Context, advisor.State, advisor.Questions) (advisor.Answers, error) {
		calls.Add(1)
		return advisorResponse(0), nil
	})
	err := quick.Check(func(seed uint16) bool {
		req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(`{"service":"github","action":"write","on_behalf_of":"other","params":{}}`))
		req.Header.Set("Authorization", "Bearer test-key")
		// Every generated grant excludes the requested principal.
		s.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"].AllowedUsers = []string{"user-" + string(rune('a'+seed%26))}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Code == 403 && r.last().PolicyDecision == "deny" && len(r.last().Record) == 0 && calls.Load() == 0 && up.Load() == 0
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGatewayAdvisorDisabledReceiptBytes(t *testing.T) {
	for _, risky := range []bool{true, false} {
		s, r, _ := advisorSetup(t, risky)
		var calls atomic.Int32
		if !risky {
			s.cfg.Advisor = advisorFunc(func(context.Context, advisor.State, advisor.Questions) (advisor.Answers, error) {
				calls.Add(1)
				return advisorResponse(1), nil
			})
		}
		if w := advisorRequest(s); w.Code != 200 {
			t.Fatal(w.Code)
		}
		d := r.last()
		_, legacyHash, err := receipt.ParseAndDigestParams(json.RawMessage(`{"message":"hello"}`))
		if err != nil {
			t.Fatal(err)
		}
		// Compare the signed preimage against the pre-advisor draft. Timing is fixed in both.
		legacy := receiptspec.Receipt{Seq: 1, TimestampUnixNS: 1, HumanPrincipal: "user-1", AgentKeyID: "test-agent",
			Service: "github", Action: "write", ParamsSHA256: legacyHash, PolicyDecision: "allow", StatusCode: 200, SignerKID: "fixed"}
		actual := legacy
		actual.ParamsSHA256 = d.ParamsSHA256
		actual.PolicyDecision = d.PolicyDecision
		actual.Error = d.Error
		expectedBytes, err := receiptspec.CanonicalHashInput(legacy)
		if err != nil {
			t.Fatal(err)
		}
		actualBytes, err := receiptspec.CanonicalHashInput(actual)
		if err != nil {
			t.Fatal(err)
		}
		if string(expectedBytes) != string(actualBytes) || len(d.Record) != 0 || calls.Load() != 0 {
			t.Fatal("disabled advisor changed receipt bytes")
		}
	}
}

func TestGatewayAdvisorReceiptFailure(t *testing.T) {
	s, r, _ := advisorSetup(t, true)
	s.cfg.AdvisorPolicy = advisor.DefaultPolicy()
	s.cfg.Advisor = advisorFunc(func(context.Context, advisor.State, advisor.Questions) (advisor.Answers, error) {
		return advisorResponse(.95), nil
	})
	r.failNext = true
	if w := advisorRequest(s); w.Code != 500 || !strings.Contains(w.Body.String(), "receipt_failed") {
		t.Fatal(w.Body.String())
	}
}
