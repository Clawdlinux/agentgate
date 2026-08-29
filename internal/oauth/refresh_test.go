package oauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRefreshFuncExchangesAndRotatesToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", got)
		}
		if got := r.Form.Get("client_id"); got != "client-id" {
			t.Errorf("client_id = %q, want client-id", got)
		}
		if got := r.Form.Get("client_secret"); got != "client-secret" {
			t.Errorf("client_secret = %q, want client-secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600,"scope":"repo read:org"}`)
	}))
	defer server.Close()

	refresh := NewRefreshFunc(&Provider{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	})

	access, rotated, expiresIn, err := refresh("old-refresh")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if access != "new-access" || rotated != "new-refresh" {
		t.Fatalf("tokens = (%q, %q), want refreshed values", access, rotated)
	}
	if expiresIn != time.Hour {
		t.Fatalf("expiresIn = %s, want %s", expiresIn, time.Hour)
	}
}
