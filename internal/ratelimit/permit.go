package ratelimit

import (
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Permit holds refundable charges until upstream concurrency admission finishes.
// Commit and Cancel are mutually exclusive and idempotent. Both accept a nil receiver.
type Permit struct {
	limiter *Limiter
	entries []*entry
	once    sync.Once
}

func (p *Permit) Commit() { p.finish(false) }

func (p *Permit) Cancel() { p.finish(true) }

func (p *Permit) finish(refund bool) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.limiter.mu.Lock()
		defer p.limiter.mu.Unlock()
		now := p.limiter.now()
		for _, e := range p.entries {
			if refund {
				e.refund(now)
			}
			e.pending--
		}
	})
}

type bucketSpec struct {
	key   bucketKey
	rps   rate.Limit
	burst int
}

// CheckKey charges only the API key bucket, before scope checks.
func (l *Limiter) CheckKey(agentKeyID string) (*Permit, time.Duration) {
	return l.check([]bucketSpec{{
		bucketKey{scope: keyScope, id: agentKeyID}, rate.Limit(l.settings.KeyRPS), l.settings.KeyBurst,
	}})
}

// CheckTenant charges tenant and service buckets after the gateway validates both scopes.
func (l *Limiter) CheckTenant(agentKeyID, tenant, service string) (*Permit, time.Duration) {
	specs := []bucketSpec{{
		bucketKey{scope: tenantScope, id: tenant}, rate.Limit(l.settings.TenantRPS), l.settings.TenantBurst,
	}}
	service = strings.ToLower(service)
	if cfg, ok := l.configs[service]; ok && cfg.Rate() != rate.Inf {
		specs = append(specs, bucketSpec{
			bucketKey{scope: serviceScope, id: agentKeyID, service: service}, cfg.Rate(), cfg.BurstSize(),
		})
	}
	return l.check(specs)
}

func (l *Limiter) check(specs []bucketSpec) (*Permit, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var entries []*entry
	var retry time.Duration
	for _, spec := range specs {
		if spec.rps <= 0 || spec.burst <= 0 {
			continue
		}
		e := l.get(spec.key, spec.rps, spec.burst, now)
		if e == nil {
			retry = time.Second
			break
		}
		if retry = e.take(now); retry > 0 {
			break
		}
		e.pending++
		entries = append(entries, e)
	}
	if retry > 0 {
		for _, e := range entries {
			e.refund(now)
			e.pending--
		}
		return nil, retry
	}
	if len(entries) == 0 {
		return nil, 0
	}
	return &Permit{limiter: l, entries: entries}, 0
}

func (e *entry) balance(now time.Time) float64 {
	tokens := e.bucket.TokensAt(now)
	e.credits = min(e.credits, max(0, float64(e.bucket.Burst())-tokens))
	return tokens + e.credits
}

func (e *entry) refund(now time.Time) {
	// CancelAt cannot refund past reservations. Credits preserve elapsed refill without rewinding the limiter's clock.
	tokens := e.bucket.TokensAt(now)
	e.credits = min(e.credits+1, max(0, float64(e.bucket.Burst())-tokens))
}

func (e *entry) take(now time.Time) time.Duration {
	available := e.balance(now)
	if e.credits >= 1 {
		e.credits--
		return 0
	}
	reservation := e.bucket.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay == 0 || available >= 1 {
		return 0
	}
	reservation.CancelAt(now)
	if delay == rate.InfDuration {
		return delay
	}
	creditDelay := math.Min(float64(delay), e.credits/float64(e.bucket.Limit())*float64(time.Second))
	return max(time.Nanosecond, delay-time.Duration(creditDelay))
}
