package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAct_CrossOriginRedirectIsRejectedAndReceipted(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "injected-secret" {
			t.Error("source credential missing")
		}
		http.Redirect(w, r, target.URL+"/private-target", http.StatusFound)
	}))
	defer source.Close()
	srv, receipts := transportGateway(t, source.URL, "{}")
	if err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: api_key, header_name: X-API-Key}
    upstream: {retry: {max_retries: 0}}
    actions:
      list_repos: {method: GET, path: /private-source}
`, source.URL))); err != nil {
		t.Fatal(err)
	}
	w := transportAct(t, srv)
	if targetCalls.Load() != 0 {
		t.Errorf("redirect target received %d requests", targetCalls.Load())
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 502 || body.Error != "upstream request failed" || body.Code != "upstream_error" {
		t.Errorf("status=%d body=%s", w.Code, w.Body)
	}
	for _, private := range []string{source.URL, target.URL, "/private-source", "/private-target", "injected-secret"} {
		if strings.Contains(w.Body.String(), private) {
			t.Error("response leaked upstream details")
		}
	}
	draft := receipts.last()
	if receipts.count() != 1 || draft.StatusCode != 502 || draft.PolicyDecision != "allow" || draft.Error != "upstream_error" {
		t.Fatalf("receipts=%d draft=%+v", receipts.count(), draft)
	}
}
