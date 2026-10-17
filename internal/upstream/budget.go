/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package upstream

import (
	"math"
	"sync"
	"time"
)

const budgetBucketWidth = 100 * time.Millisecond

type budgetBucket struct {
	tick     int64
	requests int
	retries  int
}

// retryBudget shares tokens across services. Retries never count as new requests.
type retryBudget struct {
	mu      sync.Mutex
	clock   Clock
	buckets [10]budgetBucket
	tokens  float64
	rate    float64
	last    time.Time
}

func newRetryBudget(clock Clock) *retryBudget {
	return &retryBudget{clock: clock, tokens: 10, rate: 10, last: clock.Now()}
}

func (b *retryBudget) bucket(tick int64) *budgetBucket {
	index := tick % int64(len(b.buckets))
	if index < 0 {
		index += int64(len(b.buckets))
	}
	slot := &b.buckets[index]
	if slot.tick != tick {
		*slot = budgetBucket{tick: tick}
	}
	return slot
}

func (b *retryBudget) update(request bool) (int64, int) {
	now := b.clock.Now()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * b.rate
		b.last = now
	}
	tick := now.UnixNano() / int64(budgetBucketWidth)
	if request {
		b.bucket(tick).requests++
	}
	total, retries := 0, 0
	for _, bucket := range b.buckets {
		if bucket.tick <= tick && tick-bucket.tick < int64(len(b.buckets)) {
			total += bucket.requests
			retries += bucket.retries
		}
	}
	b.rate = math.Max(10, float64(total)*0.2)
	b.tokens = math.Min(b.tokens, b.rate)
	return tick, retries
}

func (b *retryBudget) recordRequest() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.update(true)
}

func (b *retryBudget) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	tick, retries := b.update(false)
	// Token bursts must still fit the sliding-window ratio or its 10-retry floor.
	if b.tokens < 1 || float64(retries+1) > b.rate {
		return false
	}
	b.tokens--
	b.bucket(tick).retries++
	return true
}
