package vault

import (
	"container/list"
	"fmt"
	"slices"
	"sync"
	"time"
)

const (
	DefaultCacheTTL  = time.Minute
	DefaultCacheSize = 10_000
	cacheExpirySkew  = 30 * time.Second
)

type credentialKey struct {
	user, service string
}

type cacheEntry struct {
	key       credentialKey
	token     Token
	expiresAt time.Time
}

// CachedStore keeps a bounded LRU of decrypted tokens in process memory.
// Strings cannot be reliably zeroed. Delete drops cached references.
// All writes must use this decorator to keep its cache coherent.
type CachedStore struct {
	inner Store
	ttl   time.Duration
	size  int
	now   func() time.Time

	// The gate prevents an in-flight read from caching a token after a write.
	gate    sync.RWMutex
	mu      sync.Mutex
	entries map[credentialKey]*list.Element
	lru     list.List
}

var _ Store = (*CachedStore)(nil)

type CacheOption func(*CachedStore) error

// WithCacheTTL sets the cache lifetime. Zero disables caching.
func WithCacheTTL(ttl time.Duration) CacheOption {
	return func(s *CachedStore) error {
		if ttl < 0 {
			return fmt.Errorf("vault: cache TTL must not be negative")
		}
		s.ttl = ttl
		return nil
	}
}

// WithCacheSize sets the maximum number of cached token pairs.
func WithCacheSize(size int) CacheOption {
	return func(s *CachedStore) error {
		if size <= 0 {
			return fmt.Errorf("vault: cache size must be positive")
		}
		s.size = size
		return nil
	}
}

// NewCachedStore wraps a store with a 60-second, 10,000-entry cache by default.
func NewCachedStore(inner Store, opts ...CacheOption) (*CachedStore, error) {
	if inner == nil {
		return nil, fmt.Errorf("vault: cache requires a store")
	}
	s := &CachedStore{
		inner: inner, ttl: DefaultCacheTTL, size: DefaultCacheSize,
		now: time.Now, entries: make(map[credentialKey]*list.Element),
	}
	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func cloneToken(tok Token) Token {
	tok.Scopes = slices.Clone(tok.Scopes)
	return tok
}

func (s *CachedStore) remove(key credentialKey) {
	if element, ok := s.entries[key]; ok {
		delete(s.entries, key)
		s.lru.Remove(element)
	}
}

func (s *CachedStore) remember(key credentialKey, tok Token) {
	if s.ttl == 0 {
		return
	}
	now := s.now()
	deadline := now.Add(s.ttl)
	if !tok.ExpiresAt.IsZero() && tok.ExpiresAt.Add(-cacheExpirySkew).Before(deadline) {
		deadline = tok.ExpiresAt.Add(-cacheExpirySkew)
	}
	if !deadline.After(now) {
		return
	}
	s.remove(key)
	s.entries[key] = s.lru.PushFront(cacheEntry{key: key, token: cloneToken(tok), expiresAt: deadline})
	if s.lru.Len() > s.size {
		s.remove(s.lru.Back().Value.(cacheEntry).key)
	}
}

func (s *CachedStore) Get(user, service string) (Token, error) {
	if s.ttl == 0 {
		return s.inner.Get(user, service)
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	key := credentialKey{user, service}
	s.mu.Lock()
	if element, ok := s.entries[key]; ok {
		entry := element.Value.(cacheEntry)
		if s.now().Before(entry.expiresAt) {
			s.lru.MoveToFront(element)
			tok := cloneToken(entry.token)
			s.mu.Unlock()
			return tok, nil
		}
		s.remove(key)
	}
	s.mu.Unlock()

	tok, err := s.inner.Get(user, service)
	if err != nil {
		return Token{}, err
	}
	s.mu.Lock()
	s.remember(key, tok)
	s.mu.Unlock()
	return tok, nil
}

func (s *CachedStore) Put(user, service string, tok Token) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	key := credentialKey{user, service}
	s.mu.Lock()
	s.remove(key)
	s.mu.Unlock()
	if err := s.inner.Put(user, service, tok); err != nil {
		return err
	}
	s.mu.Lock()
	s.remember(key, tok)
	s.mu.Unlock()
	return nil
}

func (s *CachedStore) Delete(user, service string) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	s.mu.Lock()
	s.remove(credentialKey{user, service})
	s.mu.Unlock()
	return s.inner.Delete(user, service)
}

func (s *CachedStore) ListServices(user string) ([]string, error) {
	return s.inner.ListServices(user)
}
