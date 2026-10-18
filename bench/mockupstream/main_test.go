package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEcho(t *testing.T) {
	for _, delay := range []time.Duration{0, 5 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"worker":1}`))
			req.Header.Set("Authorization", "Bearer test-only")
			response := httptest.NewRecorder()
			echo(delay).ServeHTTP(response, req)
			var body struct {
				OK         bool  `json:"ok"`
				UpstreamNS int64 `json:"upstream_ns"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || !body.OK || body.UpstreamNS < delay.Nanoseconds() {
				t.Fatalf("unexpected response: code=%d body=%+v", response.Code, body)
			}
		})
	}
}

func TestEchoRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name, body, auth string
		status           int
	}{
		{"invalid JSON", "{", "Bearer test-only", http.StatusBadRequest},
		{"missing token", "{}", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(tc.body))
			req.Header.Set("Authorization", tc.auth)
			response := httptest.NewRecorder()
			echo(0).ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d, want %d", response.Code, tc.status)
			}
		})
	}
}
