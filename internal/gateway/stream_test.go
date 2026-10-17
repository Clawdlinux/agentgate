package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/receipt"
)

type blockingReceipts struct {
	entered chan struct{}
	release chan struct{}
	drafts  fakeReceipts
	fail    bool
}

func (f *blockingReceipts) Append(ctx context.Context, draft receipt.Draft) (receipt.Receipt, error) {
	close(f.entered)
	select {
	case <-ctx.Done():
		return receipt.Receipt{}, ctx.Err()
	case <-f.release:
		if f.fail {
			return receipt.Receipt{}, errors.New("commit failed")
		}
		return f.drafts.Append(ctx, draft)
	}
}

func streamRequest(ctx context.Context, url, accept string) *http.Request {
	req, err := http.NewRequestWithContext(ctx, "POST", url+"/v1/act", strings.NewReader(
		`{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Accept", accept)
	return req
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestAct_SSEReceiptBeforeBytesAndIncrementalFlush(t *testing.T) {
	second := make(chan struct{})
	defer func() {
		select {
		case <-second:
		default:
			close(second)
		}
	}()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("SSE accept not forwarded")
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-second:
			fmt.Fprint(w, "data: second\n\n")
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
		}
	}))
	defer up.Close()
	srv, _ := bodyGateway(t, up.URL, "{}", true)
	recorder := &blockingReceipts{entered: make(chan struct{}), release: make(chan struct{})}
	srv.cfg.Receipts = recorder
	server := httptest.NewServer(srv)
	defer server.Close()
	defer func() {
		select {
		case <-recorder.release:
		default:
			close(recorder.release)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	type result struct {
		resp *http.Response
		err  error
	}
	responses := make(chan result, 1)
	go func() {
		resp, err := server.Client().Do(streamRequest(ctx, server.URL, "text/event-stream"))
		responses <- result{resp, err}
	}()
	awaitSignal(t, recorder.entered)
	select {
	case got := <-responses:
		if got.resp != nil {
			got.resp.Body.Close()
		}
		t.Fatalf("received HTTP response before receipt commit: %v", got.err)
	case <-time.After(30 * time.Millisecond):
	}
	close(recorder.release)
	got := <-responses
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.resp.Body.Close()
	if got.resp.StatusCode != 200 || !strings.HasPrefix(got.resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d type=%s", got.resp.StatusCode, got.resp.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(got.resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "data: first\n" {
		t.Fatalf("first event=%q error=%v", first, err)
	}
	if recorder.drafts.count() != 1 || recorder.drafts.last().StatusCode != 200 ||
		recorder.drafts.last().PolicyDecision != "allow" {
		t.Fatalf("receipt=%+v", recorder.drafts.last())
	}
	close(second)
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "\ndata: second\n\n" {
		t.Fatalf("rest=%q error=%v", rest, err)
	}
}

func TestAct_SSEClientDisconnectClosesUpstream(t *testing.T) {
	closed := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer up.Close()
	srv, receipts := bodyGateway(t, up.URL, "{}", true)
	server := httptest.NewServer(srv)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	resp, err := server.Client().Do(streamRequest(ctx, server.URL, "text/event-stream"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cancel()
	awaitSignal(t, closed)
	if receipts.count() != 1 {
		t.Fatalf("receipts=%d", receipts.count())
	}
}

func TestAct_SSELimitsTerminateAndLogDigest(t *testing.T) {
	for _, tc := range []struct {
		name, settings, content, reason string
		expectedBytes                   int
	}{
		{"idle", "{stream_idle_timeout: 30ms}", "data: first\n\n", "idle_timeout", 13},
		{"size", "{max_stream_bytes: 1024}", strings.Repeat("x", 2048), "max_stream_bytes", 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed := make(chan struct{})
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.content)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			defer up.Close()
			srv, receipts := bodyGateway(t, up.URL, tc.settings, true)
			var logs bytes.Buffer
			srv.logger = slog.New(slog.NewTextHandler(&logs, nil))
			w := httptest.NewRecorder()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			srv.ServeHTTP(w, streamRequest(ctx, "", "text/event-stream"))
			awaitSignal(t, closed)
			if w.Code != 200 || w.Body.Len() != tc.expectedBytes {
				t.Fatalf("status=%d size=%d", w.Code, w.Body.Len())
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes()))
			if !strings.Contains(logs.String(), "sha256="+hash) ||
				!strings.Contains(logs.String(), "bytes="+fmt.Sprint(tc.expectedBytes)) ||
				!strings.Contains(logs.String(), "reason="+tc.reason) {
				t.Fatalf("logs=%s", &logs)
			}
			if receipts.count() != 1 || receipts.last().StatusCode != 200 || receipts.last().Error != "" {
				t.Fatalf("receipts=%d draft=%+v", receipts.count(), receipts.last())
			}
		})
	}
}

func TestAct_SSEFallback(t *testing.T) {
	for _, tc := range []struct {
		name, accept, contentType string
		stream                    bool
		status                    int
	}{
		{"no accept", "", "text/event-stream", true, 200},
		{"disabled", "text/event-stream", "text/event-stream", false, 200},
		{"not SSE", "text/event-stream", "application/json", true, 200},
		{"non success", "text/event-stream", "text/event-stream", true, 400},
		{"not acceptable", "text/event-stream;q=0", "text/event-stream", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "data: buffered\n\n")
			}))
			defer up.Close()
			srv, receipts := bodyGateway(t, up.URL, "{}", tc.stream)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, streamRequest(t.Context(), "", tc.accept))
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/json" ||
				!strings.Contains(w.Body.String(), `"body_text":"data: buffered\n\n"`) {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
			}
			if receipts.count() != 1 {
				t.Fatalf("receipts=%d", receipts.count())
			}
		})
	}
}

func TestAct_SSEReceiptFailureClosesBody(t *testing.T) {
	closed := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: secret\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer up.Close()
	srv, receipts := bodyGateway(t, up.URL, "{}", true)
	receipts.failNext = true
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	srv.ServeHTTP(w, streamRequest(ctx, "", "text/event-stream"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "data: secret") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	awaitSignal(t, closed)
}

func TestAct_SSEDoesNotUseBufferedDeadline(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-time.After(80 * time.Millisecond):
			fmt.Fprint(w, "data: second\n\n")
		case <-r.Context().Done():
		}
	}))
	defer up.Close()
	srv, _ := bodyGateway(t, up.URL, "{request_timeout: 30ms, stream_idle_timeout: 1s}", true)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, streamRequest(t.Context(), "", "text/event-stream"))
	if w.Code != 200 || w.Body.String() != "data: first\n\ndata: second\n\n" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestAct_SSESeparateLimitAndSuccessStatus(t *testing.T) {
	content := strings.Repeat("x", 2048)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, content)
	}))
	defer up.Close()
	srv, receipts := bodyGateway(t, up.URL, "{max_response_bytes: 1024, max_stream_bytes: 2048}", true)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, streamRequest(t.Context(), "", "application/json, text/event-stream; q=0.8"))
	if w.Code != 200 || w.Body.String() != content || receipts.count() != 1 || receipts.last().StatusCode != 200 {
		t.Fatalf("status=%d bytes=%d draft=%+v", w.Code, w.Body.Len(), receipts.last())
	}
}

func TestAct_SSEIdleResetsOnEventsNotBytes(t *testing.T) {
	for _, events := range []bool{true, false} {
		t.Run(fmt.Sprint(events), func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				for i := 0; i < 6; i++ {
					if events {
						fmt.Fprint(w, "data: event\r\n\r\n")
					} else {
						fmt.Fprint(w, "data: partial")
					}
					w.(http.Flusher).Flush()
					select {
					case <-time.After(30 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
				}
			}))
			defer up.Close()
			srv, _ := bodyGateway(t, up.URL, "{stream_idle_timeout: 100ms}", true)
			var logs bytes.Buffer
			srv.logger = slog.New(slog.NewTextHandler(&logs, nil))
			w := httptest.NewRecorder()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			srv.ServeHTTP(w, streamRequest(ctx, "", "text/event-stream"))
			if events {
				if strings.Count(w.Body.String(), "data: event") != 6 || !strings.Contains(logs.String(), "reason=completed") {
					t.Fatalf("body=%s logs=%s", w.Body, &logs)
				}
			} else if !strings.Contains(logs.String(), "reason=idle_timeout") {
				t.Fatalf("logs=%s", &logs)
			}
		})
	}
}

func TestAct_SSERetryProducesOneReceipt(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: success\n\n")
	}))
	defer up.Close()
	srv, receipts := bodyGateway(t, up.URL, "{}", true)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, streamRequest(t.Context(), "", "text/event-stream"))
	if w.Code != 200 || w.Body.String() != "data: success\n\n" || receipts.count() != 1 || calls.Load() != 2 {
		t.Fatalf("status=%d body=%s count=%d", w.Code, w.Body, receipts.count())
	}
}

func TestEventBoundaries(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		t.Run(fmt.Sprintf("%q", ending), func(t *testing.T) {
			var boundaries eventBoundaries
			input := "data: first" + ending + ending + "data: second" + ending + ending
			count := 0
			for _, c := range []byte(input) {
				if boundaries.scan([]byte{c}) {
					count++
				}
			}
			if count != 2 {
				t.Fatalf("boundaries=%d", count)
			}
		})
	}
}
