package advisor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTMLHeavyInputDoesNotBypassAdvisor(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > MaxInputBytes || strings.Contains(string(body), `\u003c`) {
			t.Error("unbounded or HTML-escaped input")
		}
		_ = json.NewEncoder(w).Encode(testAnswers())
	}))
	defer server.Close()
	client, err := NewHTTPAdvisor(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	state := State{"service": "github", "action": "write", "params": map[string]any{"message": strings.Repeat("<", 400000)}}
	if _, err := client.Decide(context.Background(), state, FixedQuestions()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("advisor was skipped")
	}
}

func TestHTTPInputRejectionCannotUseAllowFallback(t *testing.T) {
	for _, status := range []int{302, 400, 413, 422, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		client, err := NewHTTPAdvisor(server.URL)
		if err != nil {
			t.Fatal(err)
		}

		answer, err := client.Decide(context.Background(), State{}, FixedQuestions())
		out := DefaultPolicy().Evaluate("allow", answer, err)
		want := "require_approval"
		if status == 503 {
			want = "allow"
		}
		if out != want {
			t.Errorf("status %d: got %s want %s", status, out, want)
		}
		client.CloseIdleConnections()
		server.Close()
	}
}

func TestTruncationRespectsDenyPolicy(t *testing.T) {
	p := DefaultPolicy()
	p.Unavailable = "deny"
	answer := testAnswers()
	answer.Truncated = true
	if got := p.Evaluate("allow", answer, nil); got != "deny_by_advisor" {
		t.Fatalf("got %s", got)
	}
}
