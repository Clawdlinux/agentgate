/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package upstream

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable means the service's breaker rejected a request without dispatch.
var ErrUnavailable = errors.New("upstream unavailable")

// Clock makes cooldowns, budgets, and retry delays deterministic in tests.
type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
func (wallClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

// Options allows clock injection and an optional embedded client's behavior.
type Options struct {
	Clock  Clock
	Jitter func(time.Duration) time.Duration
	// Client preserves test transports and stricter redirect policies. Its Timeout is ignored.
	Client *http.Client
}

// Pool owns a client, transport, and breaker for each service.
// Registry settings are read on the service's first dispatch.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*clientState
	options Options
	budget  *retryBudget
}

type clientState struct {
	client    *http.Client
	transport *http.Transport
	config    Config
	breaker   *breaker
}

// New creates an empty pool with a shared retry budget.
func New(options Options) *Pool {
	if options.Clock == nil {
		options.Clock = wallClock{}
	}
	if options.Jitter == nil {
		options.Jitter = func(limit time.Duration) time.Duration {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
			if err != nil {
				return limit / 2
			}
			return time.Duration(n.Int64())
		}
	}
	return &Pool{clients: make(map[string]*clientState), options: options, budget: newRetryBudget(options.Clock)}
}

func (p *Pool) client(service string, cfg Config) *clientState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.clients[service]; c != nil {
		return c
	}
	cfg = cfg.defaults()
	dialer := &net.Dialer{Timeout: cfg.DialTimeout, KeepAlive: cfg.KeepAlive}
	tr := &http.Transport{}
	if p.options.Client != nil {
		if custom, ok := p.options.Client.Transport.(*http.Transport); ok {
			tr = custom.Clone()
		}
	}
	tr.Proxy = http.ProxyFromEnvironment
	tr.DialContext = dialer.DialContext
	tr.TLSHandshakeTimeout = cfg.TLSHandshakeTimeout
	tr.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	tr.IdleConnTimeout = cfg.IdleConnTimeout
	tr.MaxIdleConns = cfg.MaxIdleConns
	tr.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
	tr.MaxConnsPerHost = cfg.MaxConnsPerHost
	tr.ForceAttemptHTTP2 = true
	client := &http.Client{Transport: tr}
	if p.options.Client != nil {
		*client = *p.options.Client
		client.Timeout = 0
		if client.Transport == nil {
			client.Transport = tr
		}
		// Clone custom HTTP transports so services still own separate connection pools.
		if _, ok := client.Transport.(*http.Transport); ok {
			client.Transport = tr
		}
	}
	client.CheckRedirect = secureRedirectPolicy(client.CheckRedirect)
	state := &clientState{client: client, transport: tr, config: cfg, breaker: newBreaker(cfg.Breaker, p.options.Clock)}
	p.clients[service] = state
	return state
}

// CloseIdleConnections releases pooled idle connections. Active calls can finish normally.
func (p *Pool) CloseIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.clients {
		state.client.CloseIdleConnections()
	}
}

// Do dispatches one action, including bounded retries. Close the returned response body.
// The request deadline remains active until the final response body closes.
func (p *Pool) Do(service string, cfg Config, req *http.Request, idempotent bool) (*http.Response, error) {
	return p.do(service, cfg, req, idempotent, false)
}

// DoStream bounds dispatch and retries, then lifts the deadline for a successful SSE response.
// Callers must enforce stream limits and close the response body.
func (p *Pool) DoStream(service string, cfg Config, req *http.Request, idempotent bool) (*http.Response, error) {
	return p.do(service, cfg, req, idempotent, true)
}

// IsEventStream reports whether a response permits SSE pass-through.
func IsEventStream(resp *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream" && resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (p *Pool) do(service string, cfg Config, req *http.Request, idempotent, streaming bool) (*http.Response, error) {
	if err := cfg.Validate(); err != nil {
		closeBody(req.Body)
		return nil, err
	}
	state := p.client(service, cfg)
	callerCtx := req.Context()
	var ctx context.Context
	var cancel context.CancelFunc
	var dispatchTimer *time.Timer
	if streaming {
		ctx, cancel = context.WithCancel(callerCtx)
		dispatchTimer = time.AfterFunc(state.config.RequestTimeout, cancel)
		cancelContext := cancel
		cancel = func() {
			dispatchTimer.Stop()
			cancelContext()
		}
	} else {
		ctx, cancel = context.WithTimeout(callerCtx, state.config.RequestTimeout)
	}
	keepContext := false
	defer func() {
		if !keepContext {
			cancel()
		}
	}()
	p.budget.recordRequest()
	retryable := canRetry(req.Method, idempotent) && (req.Body == nil || req.Body == http.NoBody || req.GetBody != nil)
	attempt := req.Clone(ctx)
	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			closeBody(attempt.Body)
			return nil, err
		}
		permit, allowed := state.breaker.acquire()
		if !allowed {
			closeBody(attempt.Body)
			return nil, ErrUnavailable
		}
		resp, err := state.client.Do(attempt)
		if err != nil {
			if callerErr := callerCtx.Err(); callerErr != nil {
				state.breaker.release(permit)
				return nil, callerErr
			}
			if errors.Is(err, errRedirectRefused) {
				state.breaker.release(permit)
				return nil, err
			}
		}
		state.breaker.complete(permit, err != nil || resp.StatusCode >= 500)
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retryable || n >= *state.config.Retry.MaxRetries || !retryFailure(resp, err) || !p.budget.allow() {
			if err != nil {
				return nil, err
			}
			if streaming && IsEventStream(resp) {
				dispatchTimer.Stop()
			}
			resp.Body = &deadlineBody{ReadCloser: resp.Body, cancel: cancel}
			keepContext = true
			return resp, nil
		}
		delay := p.retryDelay(state.config.Retry, n, resp)
		// Discarded responses must close before sleeping. Never drain an unbounded error body.
		if resp != nil {
			closeBody(resp.Body)
		}
		if err := p.options.Clock.Sleep(ctx, delay); err != nil {
			return nil, err
		}
		attempt = req.Clone(ctx)
		if req.Body != nil && req.Body != http.NoBody {
			attempt.Body, err = req.GetBody()
			if err != nil {
				closeBody(attempt.Body)
				return nil, err
			}
		}
	}
}

func closeBody(body io.Closer) {
	if body != nil {
		body.Close()
	}
}

type deadlineBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *deadlineBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

func canRetry(method string, idempotent bool) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	case http.MethodPut, http.MethodDelete:
		return idempotent
	default:
		return false
	}
}

func retryFailure(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504
}

func (p *Pool) retryDelay(cfg RetryConfig, n int, resp *http.Response) time.Duration {
	limit := cfg.BaseDelay
	for i := 0; i < n && limit < cfg.MaxDelay; i++ {
		if limit > cfg.MaxDelay/2 {
			limit = cfg.MaxDelay
		} else {
			limit *= 2
		}
	}
	delay := p.options.Jitter(limit)
	if resp != nil {
		value := strings.TrimSpace(resp.Header.Get("Retry-After"))
		var after time.Duration
		if seconds, err := strconv.ParseUint(value, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
			// Compare before multiplication to avoid overflow on oversized headers.
			if seconds > uint64(cfg.MaxDelay/time.Second) {
				after = cfg.MaxDelay
			} else {
				after = time.Duration(seconds) * time.Second
			}
		} else if date, err := http.ParseTime(value); err == nil {
			after = min(date.Sub(p.options.Clock.Now()), cfg.MaxDelay)
		}
		delay = max(delay, after)
	}
	return delay
}
