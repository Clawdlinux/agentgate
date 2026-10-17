package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoStreamRetainsBoundsOutsideSuccessfulSSE(t *testing.T) {
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if contentType == "text/event-stream" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", contentType)
				fmt.Fprint(w, `{"partial":`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer up.Close()
			p := testPool(t, Options{})
			cfg := Config{RequestTimeout: 30 * time.Millisecond}
			cfg.Retry.MaxRetries = new(int)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", up.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := p.DoStream("test", cfg, req, false)
			if contentType == "text/event-stream" {
				if err == nil {
					resp.Body.Close()
					t.Fatal("dispatch without headers escaped request_timeout")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if _, err := io.ReadAll(resp.Body); err == nil {
				t.Fatal("non-SSE body escaped request_timeout")
			}
			if ctx.Err() != nil {
				t.Fatal("caller timeout masked missing dispatch timeout")
			}
		})
	}
}

func TestDoStreamKeepsCallerDeadline(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer up.Close()
	p := testPool(t, Options{})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", up.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.DoStream("test", Config{}, req, false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("SSE body escaped caller deadline")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("caller error=%v", ctx.Err())
	}
}

func TestResponseLimitDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.ResponseLimit() != 1<<20 || cfg.StreamLimit() != 64<<20 || cfg.StreamIdle() != 60*time.Second {
		t.Fatalf("response=%d stream=%d idle=%s", cfg.ResponseLimit(), cfg.StreamLimit(), cfg.StreamIdle())
	}
}
