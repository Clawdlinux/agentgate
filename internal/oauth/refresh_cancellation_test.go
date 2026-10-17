package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshHTTPExchangeHonorsContext(t *testing.T) {
	for _, body := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[body], func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				if body {
					_, _ = w.Write([]byte("{"))
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
				close(canceled)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			refresh := NewRefreshFunc(&Provider{TokenURL: server.URL})
			done := make(chan error, 1)
			go func() {
				_, _, _, err := refresh(ctx, "refresh")
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("provider exchange never started")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("HTTP refresh cancellation = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("provider exchange ignored cancellation")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("provider connection remained open after cancellation")
			}
		})
	}
}

func TestTokenHTTPClientTimeout(t *testing.T) {
	if tokenHTTPClient.Timeout != 15*time.Second {
		t.Fatalf("token HTTP timeout = %s", tokenHTTPClient.Timeout)
	}
}
