/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package upstream isolates service connections and bounds retries and failures.
package upstream

import (
	"fmt"
	"time"
)

// Config contains optional per-service transport settings. Zero values use defaults.
type Config struct {
	RequestTimeout        time.Duration `yaml:"request_timeout,omitempty"`
	DialTimeout           time.Duration `yaml:"dial_timeout,omitempty"`
	KeepAlive             time.Duration `yaml:"keep_alive,omitempty"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout,omitempty"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout,omitempty"`
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout,omitempty"`
	MaxIdleConns          int           `yaml:"max_idle_conns,omitempty"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_conns_per_host,omitempty"`
	MaxConnsPerHost       int           `yaml:"max_conns_per_host,omitempty"`
	Retry                 RetryConfig   `yaml:"retry,omitempty"`
	Breaker               BreakerConfig `yaml:"breaker,omitempty"`
}

// RetryConfig bounds additional attempts and their delays.
type RetryConfig struct {
	MaxRetries *int          `yaml:"max_retries,omitempty"` // nil uses 2; 0 disables retries
	BaseDelay  time.Duration `yaml:"base_delay,omitempty"`
	MaxDelay   time.Duration `yaml:"max_delay,omitempty"`
}

// BreakerConfig controls consecutive failures and probe timing.
type BreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold,omitempty"`
	Cooldown         time.Duration `yaml:"cooldown,omitempty"`
}

func (c Config) defaults() Config {
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.KeepAlive == 0 {
		c.KeepAlive = 30 * time.Second
	}
	if c.TLSHandshakeTimeout == 0 {
		c.TLSHandshakeTimeout = 5 * time.Second
	}
	if c.ResponseHeaderTimeout == 0 {
		c.ResponseHeaderTimeout = 30 * time.Second
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = 90 * time.Second
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 256
	}
	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = 32
	}
	if c.Retry.MaxRetries == nil {
		n := 2
		c.Retry.MaxRetries = &n
	} else {
		n := *c.Retry.MaxRetries
		c.Retry.MaxRetries = &n
	}
	if c.Retry.BaseDelay == 0 {
		c.Retry.BaseDelay = 100 * time.Millisecond
	}
	if c.Retry.MaxDelay == 0 {
		c.Retry.MaxDelay = 2 * time.Second
	}
	if c.Breaker.FailureThreshold == 0 {
		c.Breaker.FailureThreshold = 5
	}
	if c.Breaker.Cooldown == 0 {
		c.Breaker.Cooldown = 30 * time.Second
	}
	return c
}

// Validate rejects settings that would disable bounds or produce invalid delays.
func (c Config) Validate() error {
	durations := []struct {
		name  string
		value time.Duration
	}{
		{"request_timeout", c.RequestTimeout}, {"dial_timeout", c.DialTimeout},
		{"keep_alive", c.KeepAlive}, {"tls_handshake_timeout", c.TLSHandshakeTimeout},
		{"response_header_timeout", c.ResponseHeaderTimeout}, {"idle_conn_timeout", c.IdleConnTimeout},
		{"retry.base_delay", c.Retry.BaseDelay}, {"retry.max_delay", c.Retry.MaxDelay},
		{"breaker.cooldown", c.Breaker.Cooldown},
	}
	for _, d := range durations {
		if d.value < 0 {
			return fmt.Errorf("upstream: %s must not be negative", d.name)
		}
	}
	if c.MaxIdleConns < 0 || c.MaxIdleConnsPerHost < 0 || c.MaxConnsPerHost < 0 ||
		c.Breaker.FailureThreshold < 0 || (c.Retry.MaxRetries != nil && *c.Retry.MaxRetries < 0) {
		return fmt.Errorf("upstream: connection, retry, and failure counts must not be negative")
	}
	resolved := c.defaults()
	if resolved.Retry.BaseDelay > resolved.Retry.MaxDelay {
		return fmt.Errorf("upstream: retry.base_delay must not exceed retry.max_delay")
	}
	return nil
}
