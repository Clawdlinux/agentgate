package registry

import (
	"testing"
	"time"
)

func TestRegistry_UpstreamSettings(t *testing.T) {
	r := New()
	err := r.LoadBytes([]byte(`
services:
  custom:
    base_url: https://example.com
    upstream:
      request_timeout: 15s
      dial_timeout: 2s
      keep_alive: 20s
      tls_handshake_timeout: 3s
      response_header_timeout: 10s
      idle_conn_timeout: 60s
      max_idle_conns: 128
      max_idle_conns_per_host: 64
      max_conns_per_host: 100
      max_response_bytes: 2048
      max_stream_bytes: 1048576
      stream_idle_timeout: 15s
      retry:
        max_retries: 0
        base_delay: 50ms
        max_delay: 1s
      breaker:
        failure_threshold: 3
        cooldown: 10s
    actions:
      replace:
        method: PUT
        path: /resource
        idempotent: true
        stream: true
`))
	if err != nil {
		t.Fatal(err)
	}
	svc, action, err := r.GetAction("custom", "replace")
	if err != nil {
		t.Fatal(err)
	}
	cfg := svc.Upstream
	if cfg.RequestTimeout != 15*time.Second || cfg.ResponseHeaderTimeout != 10*time.Second ||
		cfg.MaxIdleConnsPerHost != 64 || cfg.MaxConnsPerHost != 100 ||
		cfg.Retry.MaxRetries == nil || *cfg.Retry.MaxRetries != 0 ||
		cfg.Breaker.FailureThreshold != 3 || !action.Idempotent || !action.Stream ||
		cfg.MaxResponseBytes != 2048 || cfg.MaxStreamBytes != 1048576 || cfg.StreamIdleTimeout != 15*time.Second {
		t.Fatalf("settings=%+v action=%+v", cfg, action)
	}
}

func TestRegistry_RejectsInvalidUpstreamSettings(t *testing.T) {
	for _, setting := range []string{
		"request_timeout: -1s", "dial_timeout: -1s", "keep_alive: -1s",
		"tls_handshake_timeout: -1s", "response_header_timeout: -1s", "idle_conn_timeout: -1s",
		"max_idle_conns: -1", "max_idle_conns_per_host: -1", "max_conns_per_host: -1",
		"retry: {max_retries: -1}", "retry: {base_delay: -1s}", "retry: {max_delay: -1s}",
		"retry: {base_delay: 3s, max_delay: 1s}",
		"breaker: {failure_threshold: -1}", "breaker: {cooldown: -1s}",
		"max_response_bytes: -1", "max_response_bytes: 1023", "max_response_bytes: 67108865",
		"max_stream_bytes: -1", "max_stream_bytes: 1023", "max_stream_bytes: 1073741825",
		"stream_idle_timeout: -1s",
	} {
		t.Run(setting, func(t *testing.T) {
			r := New()
			if err := r.LoadBytes([]byte("services:\n  bad:\n    upstream: {" + setting + "}\n")); err == nil {
				t.Fatal("invalid settings accepted")
			}
			if r.Count() != 0 {
				t.Fatal("invalid service was installed")
			}
		})
	}
}
