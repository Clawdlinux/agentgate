package vault

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type tokenPair struct {
	user, service string
}

type countingStore struct {
	mu          sync.Mutex
	tokens      map[tokenPair]Token
	gets, puts  int
	putErr      error
	deleteErr   error
	getStarted  chan struct{}
	getContinue chan struct{}
}

func newCountingStore() *countingStore {
	return &countingStore{tokens: make(map[tokenPair]Token)}
}

func (s *countingStore) Get(user, service string) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	tok, ok := s.tokens[tokenPair{user, service}]
	if s.getStarted != nil {
		close(s.getStarted)
		<-s.getContinue
		s.getStarted = nil
	}
	if !ok {
		return Token{}, ErrTokenNotFound
	}
	return tok, nil
}

func (s *countingStore) Put(user, service string, tok Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.putErr != nil {
		return s.putErr
	}
	s.tokens[tokenPair{user, service}] = tok
	return nil
}

func (s *countingStore) Delete(user, service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.tokens, tokenPair{user, service})
	return nil
}

func (s *countingStore) ListServices(user string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var services []string
	for key := range s.tokens {
		if key.user == user {
			services = append(services, key.service)
		}
	}
	return services, nil
}

func newTestCache(t *testing.T, inner Store, opts ...CacheOption) *CachedStore {
	t.Helper()
	store, err := NewCachedStore(inner, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestCachedStoreHitAndKeyIsolation(t *testing.T) {
	inner := newCountingStore()
	_ = inner.Put("a:b", "c", Token{AccessToken: "first"})
	_ = inner.Put("a", "b:c", Token{AccessToken: "second"})
	store := newTestCache(t, inner)
	for range 3 {
		for _, tc := range []struct{ user, service, want string }{
			{"a:b", "c", "first"},
			{"a", "b:c", "second"},
		} {
			tok, err := store.Get(tc.user, tc.service)
			if err != nil || tok.AccessToken != tc.want {
				t.Fatalf("Get = %v, %v", tok, err)
			}
		}
	}
	if inner.gets != 2 {
		t.Fatalf("inner Gets = %d, want 2", inner.gets)
	}
}

func TestCachedStoreLifetime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		expiry time.Duration
		life   time.Duration
	}{
		{"ttl", time.Hour, time.Minute},
		{"skew", 50 * time.Second, 20 * time.Second},
		{"no-expiry", 0, time.Minute},
		{"within-skew", 20 * time.Second, 0},
		{"expired", -time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
			inner := newCountingStore()
			tok := Token{AccessToken: "token"}
			if tc.expiry != 0 {
				tok.ExpiresAt = now.Add(tc.expiry)
			}
			_ = inner.Put("u", "s", tok)
			store := newTestCache(t, inner)
			store.now = func() time.Time { return now }
			_, _ = store.Get("u", "s")
			if tc.life > 0 {
				now = now.Add(tc.life - time.Nanosecond)
				_, _ = store.Get("u", "s")
				if inner.gets != 1 {
					t.Fatal("cache expired early")
				}
				now = now.Add(time.Nanosecond)
			}
			_, _ = store.Get("u", "s")
			if inner.gets != 2 {
				t.Fatalf("inner Gets = %d, want 2", inner.gets)
			}
		})
	}
}

func TestCachedStoreWriteThrough(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner)
	if err := store.Put("u", "s", Token{AccessToken: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("u", "s", Token{AccessToken: "new"}); err != nil {
		t.Fatal(err)
	}
	tok, err := store.Get("u", "s")
	if err != nil || tok.AccessToken != "new" || inner.gets != 0 {
		t.Fatalf("write-through Get = %v, %v; inner Gets = %d", tok, err, inner.gets)
	}
	services, err := store.ListServices("u")
	if err != nil || len(services) != 1 || services[0] != "s" {
		t.Fatalf("ListServices = %v, %v", services, err)
	}
	if err := store.Delete("u", "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("u", "s"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
	if len(store.entries) != 0 {
		t.Fatal("Delete retained plaintext")
	}
}

func TestCachedStoreWriteFailureInvalidates(t *testing.T) {
	for _, operation := range []string{"put", "delete"} {
		t.Run(operation, func(t *testing.T) {
			inner := newCountingStore()
			store := newTestCache(t, inner)
			_ = store.Put("u", "s", Token{AccessToken: "old"})
			want := errors.New("database unavailable")
			var err error
			if operation == "put" {
				inner.putErr = want
				err = store.Put("u", "s", Token{AccessToken: "new"})
			} else {
				inner.deleteErr = want
				err = store.Delete("u", "s")
			}
			if !errors.Is(err, want) || len(store.entries) != 0 {
				t.Fatalf("write = %v; entries = %d", err, len(store.entries))
			}
			tok, err := store.Get("u", "s")
			if err != nil || tok.AccessToken != "old" || inner.gets != 1 {
				t.Fatalf("Get = %v, %v; inner Gets = %d", tok, err, inner.gets)
			}
		})
	}
}

func TestCachedStoreLRU(t *testing.T) {
	inner := newCountingStore()
	for _, service := range []string{"a", "b", "c"} {
		_ = inner.Put("u", service, Token{AccessToken: service})
	}
	store := newTestCache(t, inner, WithCacheSize(2))
	for _, service := range []string{"a", "b", "a", "c", "a"} {
		_, _ = store.Get("u", service)
	}
	if inner.gets != 3 || len(store.entries) != 2 {
		t.Fatalf("Gets = %d, entries = %d", inner.gets, len(store.entries))
	}
	_, _ = store.Get("u", "b")
	if inner.gets != 4 || len(store.entries) != 2 {
		t.Fatal("least recently used entry was not evicted")
	}
}

func TestCachedStoreDisabledAndErrors(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner, WithCacheTTL(0))
	_ = store.Put("u", "s", Token{AccessToken: "token"})
	for range 2 {
		_, _ = store.Get("u", "s")
	}
	if inner.gets != 2 || len(store.entries) != 0 {
		t.Fatal("disabled cache retained a token")
	}
	store = newTestCache(t, inner)
	for range 2 {
		if _, err := store.Get("u", "missing"); !errors.Is(err, ErrTokenNotFound) {
			t.Fatalf("Get = %v", err)
		}
	}
	if inner.gets != 4 {
		t.Fatal("Get errors were cached")
	}
	for _, opt := range []CacheOption{WithCacheTTL(-time.Second), WithCacheSize(0), WithCacheSize(-1)} {
		if _, err := NewCachedStore(inner, opt); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	if _, err := NewCachedStore(nil); err == nil {
		t.Fatal("nil store accepted")
	}
}

func TestCachedStoreScopesAreCopied(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner)
	tok := Token{AccessToken: "token", Scopes: []string{"read"}}
	_ = store.Put("u", "s", tok)
	tok.Scopes[0] = "changed"
	got, _ := store.Get("u", "s")
	if got.Scopes[0] != "read" {
		t.Fatal("Put retained caller scopes")
	}
	got.Scopes[0] = "changed-again"
	got, _ = store.Get("u", "s")
	if got.Scopes[0] != "read" {
		t.Fatal("Get exposed cached scopes")
	}
}

func TestCachedStoreConcurrentReadWrite(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 20 {
				_ = store.Put("u", "s", Token{AccessToken: "token", Scopes: []string{"read"}})
				tok, _ := store.Get("u", "s")
				if len(tok.Scopes) > 0 {
					tok.Scopes[0] = "private"
				}
				_ = store.Delete("u", "s")
			}
		})
	}
	wg.Wait()
}

func TestCachedStoreReadCannotRepopulateAfterWrite(t *testing.T) {
	for _, operation := range []string{"put", "delete", "failed-put", "failed-delete"} {
		t.Run(operation, func(t *testing.T) {
			inner := newCountingStore()
			_ = inner.Put("u", "s", Token{AccessToken: "old"})
			inner.getStarted = make(chan struct{})
			inner.getContinue = make(chan struct{})
			if operation == "failed-put" {
				inner.putErr = errors.New("write failed")
			}
			if operation == "failed-delete" {
				inner.deleteErr = errors.New("delete failed")
			}
			store := newTestCache(t, inner)
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				_, _ = store.Get("u", "s")
			}()
			awaitSignal(t, inner.getStarted)
			writeDone := make(chan struct{})
			go func() {
				defer close(writeDone)
				if operation == "put" || operation == "failed-put" {
					_ = store.Put("u", "s", Token{AccessToken: "new"})
				} else {
					_ = store.Delete("u", "s")
				}
			}()
			close(inner.getContinue)
			awaitSignal(t, readDone)
			awaitSignal(t, writeDone)
			if operation != "put" && len(store.entries) != 0 {
				t.Fatal("read repopulated the cache after invalidation")
			}
			tok, err := store.Get("u", "s")
			if operation == "delete" {
				if !errors.Is(err, ErrTokenNotFound) {
					t.Fatalf("Get after Delete = %v", err)
				}
			} else {
				want := "old"
				if operation == "put" {
					want = "new"
				}
				if err != nil || tok.AccessToken != want {
					t.Fatalf("Get = %v, %v; want %s", tok, err, want)
				}
			}
		})
	}
}
