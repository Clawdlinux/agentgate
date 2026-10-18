package ratelimit

import (
	"container/list"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"gopkg.in/yaml.v3"
)

// Config defines rate limit for a service.
type Config struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	RequestsPerMinute float64 `yaml:"requests_per_minute"`
	RequestsPerHour   float64 `yaml:"requests_per_hour"`
	Burst             int     `yaml:"burst"`
}

// UnmarshalYAML prevents YAML's numeric coercion from silently truncating fractional bursts.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type plain Config
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	var fields struct {
		Burst yaml.Node `yaml:"burst"`
	}
	if err := node.Decode(&fields); err != nil {
		return err
	}
	burst := &fields.Burst
	for burst.Kind == yaml.AliasNode {
		burst = burst.Alias
	}
	if burst.Kind != 0 && burst.Tag != "!!int" {
		return fmt.Errorf("rate_limit burst must be an integer")
	}
	*c = Config(decoded)
	return nil
}

// Validate rejects ambiguous or invalid service settings.
func (c Config) Validate() error {
	rates := 0
	for _, value := range []float64{c.RequestsPerSecond, c.RequestsPerMinute, c.RequestsPerHour} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("rate_limit rates must be finite and non-negative")
		}
		if value > 0 {
			rates++
		}
	}
	if rates > 1 || c.Burst < 0 {
		return fmt.Errorf("rate_limit requires at most 1 positive rate and a non-negative burst")
	}
	if rates == 1 && c.Rate() <= 0 {
		return fmt.Errorf("rate_limit rate is too small")
	}
	return nil
}

// Rate returns the rate.Limit based on the config.
func (c Config) Rate() rate.Limit {
	if c.RequestsPerSecond > 0 {
		return rate.Limit(c.RequestsPerSecond)
	}
	if c.RequestsPerMinute > 0 {
		return rate.Limit(c.RequestsPerMinute / 60)
	}
	if c.RequestsPerHour > 0 {
		return rate.Limit(c.RequestsPerHour / 3600)
	}
	return rate.Inf
}

// BurstSize returns the burst size, defaulting to 10.
func (c Config) BurstSize() int {
	if c.Burst > 0 {
		return c.Burst
	}
	return 10
}

// Settings controls gateway-wide admission. Zero rate or burst disables that bucket.
type Settings struct {
	KeyRPS            float64
	KeyBurst          int
	TenantRPS         float64
	TenantBurst       int
	TenantMaxInflight int
}

// DefaultSettings enables per-key and per-tenant admission.
func DefaultSettings() Settings {
	return Settings{KeyRPS: 20, KeyBurst: 40, TenantRPS: 100, TenantBurst: 200, TenantMaxInflight: 32}
}

type bucketScope uint8

const (
	keyScope bucketScope = iota
	tenantScope
	serviceScope
)

// Separate fields prevent collisions between identities containing separators.
type bucketKey struct {
	scope   bucketScope
	id      string
	service string
}

type entry struct {
	key      bucketKey
	bucket   *rate.Limiter
	slots    chan struct{}
	lastUsed time.Time
	credits  float64
	pending  int
}

// Limiter shares a bounded LRU across key, tenant, and key-service buckets.
// Pending permits and active semaphores pin their entries. Depleted buckets also remain cached.
type Limiter struct {
	mu         sync.Mutex
	configs    map[string]Config
	settings   Settings
	now        func() time.Time
	entries    map[bucketKey]*list.Element
	lru        *list.List
	maxEntries int
	nextSweep  time.Time
}

type Option func(*Limiter)

func WithSettings(settings Settings) Option {
	return func(l *Limiter) { l.settings = settings }
}

// WithClock supplies explicit reservation times, including deterministic test clocks.
func WithClock(now func() time.Time) Option {
	return func(l *Limiter) { l.now = now }
}

// New copies service settings. Global defaults are enabled unless overridden.
func New(configs map[string]Config, options ...Option) *Limiter {
	l := &Limiter{
		configs: make(map[string]Config, len(configs)), settings: DefaultSettings(), now: time.Now,
		entries: make(map[bucketKey]*list.Element), lru: list.New(), maxEntries: 100_000,
	}
	for name, cfg := range configs {
		l.configs[strings.ToLower(name)] = cfg
	}
	for _, option := range options {
		option(l)
	}
	return l
}

// Allow checks the per-key, per-service bucket for standalone callers.
// Gateway admission uses CheckKey and CheckTenant instead.
func (l *Limiter) Allow(agentKeyID, service string) bool {
	service = strings.ToLower(service)
	cfg, ok := l.configs[service]
	if !ok || cfg.Rate() == rate.Inf {
		return true
	}
	permit, retry := l.check([]bucketSpec{{
		bucketKey{scope: serviceScope, id: agentKeyID, service: service}, cfg.Rate(), cfg.BurstSize(),
	}})
	permit.Commit()
	return retry == 0
}

// Check reserves 1 token in every enabled bucket. Zero means admission succeeded.
// Rejections cancel all reservations, so retries don't postpone recovery.
// Gateway admission uses CheckKey and CheckTenant around scope validation.
func (l *Limiter) Check(agentKeyID, tenant, service string) time.Duration {
	key, retry := l.CheckKey(agentKeyID)
	if retry > 0 {
		return retry
	}
	tenantPermit, retry := l.CheckTenant(agentKeyID, tenant, service)
	if retry > 0 {
		key.Cancel()
		return retry
	}
	key.Commit()
	tenantPermit.Commit()
	return 0
}

// Acquire takes a tenant semaphore slot without waiting. The release function is idempotent.
// A concurrency rejection retries after 1s because completion times are unknown.
func (l *Limiter) Acquire(tenant string) (func(), time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.settings.TenantMaxInflight <= 0 {
		return func() {}, 0
	}
	now := l.now()
	e := l.get(bucketKey{scope: tenantScope, id: tenant}, rate.Limit(l.settings.TenantRPS), l.settings.TenantBurst, now)
	if e == nil {
		return nil, time.Second
	}
	select {
	case e.slots <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				l.mu.Lock()
				defer l.mu.Unlock()
				<-e.slots
				e.lastUsed = l.now()
				l.lru.MoveToFront(l.entries[e.key])
			})
		}, 0
	default:
		return nil, time.Second
	}
}

func (l *Limiter) get(key bucketKey, rps rate.Limit, burst int, now time.Time) *entry {
	if !now.Before(l.nextSweep) {
		// Sweep lazily at most once per minute. No background goroutine needs shutdown.
		for element := l.lru.Back(); element != nil; {
			previous := element.Prev()
			e := element.Value.(*entry)
			if now.Sub(e.lastUsed) >= 10*time.Minute && e.evictable(now) {
				l.remove(element)
			}
			element = previous
		}
		l.nextSweep = now.Add(time.Minute)
	}
	if element := l.entries[key]; element != nil {
		e := element.Value.(*entry)
		e.lastUsed = now
		l.lru.MoveToFront(element)
		return e
	}
	if len(l.entries) >= l.maxEntries {
		evicted := false
		for scanned := 0; scanned < min(64, len(l.entries)); scanned++ {
			oldest := l.lru.Back()
			if oldest.Value.(*entry).evictable(now) {
				l.remove(oldest)
				evicted = true
				break
			}
			l.lru.MoveToFront(oldest)
		}
		if !evicted {
			return nil
		}
	}
	e := &entry{key: key, lastUsed: now}
	if rps > 0 && burst > 0 {
		e.bucket = rate.NewLimiter(rps, burst)
	}
	if key.scope == tenantScope && l.settings.TenantMaxInflight > 0 {
		e.slots = make(chan struct{}, l.settings.TenantMaxInflight)
	}
	l.entries[key] = l.lru.PushFront(e)
	return e
}

func (e *entry) evictable(now time.Time) bool {
	return e.pending == 0 && len(e.slots) == 0 && (e.bucket == nil || e.balance(now) >= float64(e.bucket.Burst()))
}

func (l *Limiter) remove(element *list.Element) {
	delete(l.entries, element.Value.(*entry).key)
	l.lru.Remove(element)
}
