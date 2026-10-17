/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package upstream

import (
	"sync"
	"time"
)

type breakerState uint8

const (
	closed breakerState = iota
	open
	halfOpen
)

type breaker struct {
	mu         sync.Mutex
	config     BreakerConfig
	clock      Clock
	state      breakerState
	failures   int
	openedAt   time.Time
	generation uint64
}

func newBreaker(cfg BreakerConfig, clock Clock) *breaker {
	return &breaker{config: cfg, clock: clock}
}

func (b *breaker) acquire() (uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case open:
		if b.clock.Now().Sub(b.openedAt) < b.config.Cooldown {
			return 0, false
		}
		b.state = halfOpen
		b.generation++
	case halfOpen:
		return 0, false
	}
	return b.generation, true
}

func (b *breaker) complete(generation uint64, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// An earlier in-flight call must not reset a newer open or probe state.
	if generation != b.generation || b.state == open {
		return
	}
	if !failed {
		b.failures = 0
		if b.state == halfOpen {
			b.state = closed
			b.generation++
		}
		return
	}
	b.failures++
	if b.state == halfOpen || b.failures >= b.config.FailureThreshold {
		b.state = open
		b.openedAt = b.clock.Now()
		b.generation++
	}
}
