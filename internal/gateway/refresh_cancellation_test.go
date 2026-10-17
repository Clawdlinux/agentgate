package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/vault"
)

func TestActRefreshCancellationIsNotTokenExpired(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[deadline], func(t *testing.T) {
			srv, store, receipts := testSetup(t)
			_ = store.Put("user-1", "github", vault.Token{
				AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute),
			})
			started, release := make(chan struct{}), make(chan struct{})
			refresh := func(context.Context, string) (string, string, time.Duration, error) {
				close(started)
				<-release
				return "new", "rotated", time.Hour, nil
			}
			srv.cfg.Refreshers = map[string]vault.RefreshFunc{"github": refresh}
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
				`{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`)).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer test-key")
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				srv.ServeHTTP(w, req)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("refresh never started")
			}
			if !deadline {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("gateway didn't honor waiter cancellation")
			}
			close(release)
			if _, err := vault.GetOrRefresh(context.Background(), store, "user-1", "github", refresh); err != nil {
				t.Fatal(err)
			}
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusGatewayTimeout || body.Code != "vault_refresh_timeout" {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body)
			}
			if body.Error != "vault refresh timed out or was canceled" {
				t.Fatalf("unexpected cancellation message: %q", body.Error)
			}
			if receipts.count() != 1 || receipts.last().Error != "vault_refresh_timeout" {
				t.Fatalf("receipt = %+v", receipts.last())
			}
		})
	}
}
