package ratelimit

import (
	"encoding/json"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// Config defines rate limit for a service.
type Config struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	RequestsPerMinute float64 `yaml:"requests_per_minute"`
	RequestsPerHour   float64 `yaml:"requests_per_hour"`
	Burst             int     `yaml:"burst"`
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

// Limiter implements per-(agent, service) rate limiting.
type Limiter struct {
	configs  map[string]Config // service -> config
	limiters sync.Map          // "agentKeyID:service" -> *rate.Limiter
}

// New creates a rate limiter from service configs.
func New(configs map[string]Config) *Limiter {
	return &Limiter{configs: configs}
}

// Allow checks if a request is allowed for the given agent and service.
func (l *Limiter) Allow(agentKeyID, service string) bool {
	key := agentKeyID + ":" + service
	lim, ok := l.limiters.Load(key)
	if !ok {
		cfg, exists := l.configs[service]
		if !exists {
			return true // no rate limit configured
		}
		newLim := rate.NewLimiter(cfg.Rate(), cfg.BurstSize())
		actual, _ := l.limiters.LoadOrStore(key, newLim)
		lim = actual
	}
	return lim.(*rate.Limiter).Allow()
}

// Middleware wraps an http.Handler with rate limiting.
// It requires the agent key ID and service to be extractable from the request.
// This is a generic middleware that rejects with 429 if rate exceeded.
func Middleware(limiter *Limiter, extractKeyAndService func(r *http.Request) (string, string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID, service := extractKeyAndService(r)
			if keyID != "" && service != "" {
				if !limiter.Allow(keyID, service) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(http.StatusTooManyRequests)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "rate limit exceeded",
						"code":  "rate_limited",
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
