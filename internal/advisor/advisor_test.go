package advisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func testAnswers() Answers {
	return Answers{CascadeThreshold: .8, Attempts: []TierAnswer{{
		Tier: "A", ModelID: "heuristic", Revision: strings.Repeat("a", 40),
		WeightsSHA256: strings.Repeat("b", 64), Confidence: .95,
		Probabilities: map[string]float64{"task_consistent": .9, "personal_data": .1, "risk": .2},
	}}}
}

func TestHTTPAdvisor(t *testing.T) {
	good, _ := json.Marshal(testAnswers())
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		delay   time.Duration
		wantErr bool
	}{
		{"valid", 200, string(good), 0, false},
		{"oversized", 200, strings.Repeat("x", MaxResponseBytes+1), 0, true},
		{"malformed", 200, `{}`, 0, true},
		{"trailing", 200, string(good) + ` {}`, 0, true},
		{"unavailable", 503, `{}`, 0, true},
		{"partial_evidence", 503, string(good), 0, true},
		{"deadline", 200, string(good), 200 * time.Millisecond, true},
		{"redirect", 302, `{}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/decide" || r.Method != "POST" {
					t.Error("wrong endpoint")
				}
				time.Sleep(tc.delay)
				w.Header().Set("Location", "/v1/decide")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			client, err := NewHTTPAdvisor(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			answer, err := client.Decide(context.Background(), State{"action": "write"}, FixedQuestions())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if tc.name == "partial_evidence" && len(answer.Attempts) != 1 {
				t.Fatal("discarded completed tier evidence")
			}
			if tc.delay > 0 && time.Since(start) > 190*time.Millisecond {
				t.Fatal("timeout exceeded")
			}
		})
	}
}

func TestURLRestrictions(t *testing.T) {
	for _, u := range []string{"https://example.com", "http://169.254.169.254", "http://user@localhost", "http://localhost/path", "ftp://localhost", "http://localhost?x=y", "http://127.0.0.2"} {
		if _, err := NewHTTPAdvisor(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	for _, u := range []string{"http://localhost:8081", "http://127.0.0.1:8081", "http://[::1]:8081", "http://decision-advisor.ns.svc.cluster.local:8081"} {
		if _, err := NewHTTPAdvisor(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestEscalateOnlyProperty(t *testing.T) {
	p := DefaultPolicy()
	err := quick.Check(func(score uint8, deny bool) bool {
		a := testAnswers()
		a.Attempts[0].Probabilities["risk"] = float64(score) / 255
		base := "allow"
		if deny {
			base = "deny"
		}
		out := p.Evaluate(base, a, nil)
		return !deny || out == "deny"
	}, &quick.Config{MaxCount: 1000})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPolicyAndUnavailable(t *testing.T) {
	p := DefaultPolicy()
	for _, tc := range []struct {
		risk float64
		want string
	}{{.1, "allow"}, {.7, "require_approval"}, {.95, "deny_by_advisor"}} {
		a := testAnswers()
		a.Attempts[0].Probabilities["risk"] = tc.risk
		if got := p.Evaluate("allow", a, nil); got != tc.want {
			t.Errorf("%f: %s", tc.risk, got)
		}
	}
	for _, tc := range []struct{ mode, want string }{{"allow_with_flag", "allow"}, {"require_approval", "require_approval"}, {"deny", "deny_by_advisor"}} {
		p.Unavailable = tc.mode
		if got := p.Evaluate("allow", Answers{}, context.DeadlineExceeded); got != tc.want {
			t.Errorf("%s: %s", tc.mode, got)
		}
	}
}

func TestNoop(t *testing.T) {
	answers, err := (NoopAdvisor{}).Decide(context.Background(), State{}, FixedQuestions())
	if err != nil {
		t.Fatal(err)
	}
	p := DefaultPolicy()
	p.Unavailable = "deny"
	if p.Evaluate("allow", answers, err) != "allow" {
		t.Fatal("noop escalated")
	}
}

func TestCascadeCannotEraseEarlierRisk(t *testing.T) {
	a := testAnswers()
	a.Attempts[0].Probabilities["risk"] = .95
	next := testAnswers().Attempts[0]
	next.Tier = "B"
	a.Attempts = append(a.Attempts, next)
	if DefaultPolicy().Evaluate("allow", a, nil) != "deny_by_advisor" {
		t.Fatal("later tier erased risk")
	}
}

func TestHTTPAdvisorCallerDeadlineAndInputLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer srv.Close()
	client, err := NewHTTPAdvisor(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.Decide(ctx, State{}, FixedQuestions()); err == nil {
		t.Fatal("ignored caller deadline")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("caller deadline exceeded")
	}
	if _, err := client.Decide(context.Background(), State{"text": strings.Repeat("x", MaxInputBytes)}, FixedQuestions()); err == nil {
		t.Fatal("accepted oversized input")
	}
}
