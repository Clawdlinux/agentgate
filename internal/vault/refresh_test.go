package vault

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetOrRefresh_FreshToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken: "still-valid",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	refreshCalled := false
	tok, err := GetOrRefresh(context.Background(), store, "user-1", "github", func(_ context.Context, rt string) (string, string, time.Duration, error) {
		refreshCalled = true
		return "", "", 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalled {
		t.Fatal("refresh should not be called for fresh token")
	}
	if tok.AccessToken != "still-valid" {
		t.Fatalf("got %s", tok.AccessToken)
	}
}

func TestGetOrRefresh_ExpiredToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	tok, err := GetOrRefresh(context.Background(), store, "user-1", "github", func(_ context.Context, rt string) (string, string, time.Duration, error) {
		if rt != "refresh-123" {
			t.Fatalf("unexpected refresh token: %s", rt)
		}
		return "new-access", "new-refresh", time.Hour, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "new-access" {
		t.Fatalf("access = %s", tok.AccessToken)
	}

	// Verify stored token was updated.
	stored, _ := store.Get("user-1", "github")
	if stored.AccessToken != "new-access" {
		t.Fatalf("stored access = %s", stored.AccessToken)
	}
}

func TestGetOrRefresh_RefreshFails(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken:  "expired",
		RefreshToken: "bad-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	_, err := GetOrRefresh(context.Background(), store, "user-1", "github", func(_ context.Context, rt string) (string, string, time.Duration, error) {
		return "", "", 0, errors.New("refresh denied")
	})
	if err == nil {
		t.Fatal("expected error when refresh fails")
	}
}

func TestGetOrRefresh_NoRefreshToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})

	_, err := GetOrRefresh(context.Background(), store, "user-1", "github", nil)
	if err == nil {
		t.Fatal("expected error when no refresh token")
	}
}

// Done signals that a caller has joined the flight and started waiting.
type waitingContext struct {
	context.Context
	joined chan<- struct{}
	once   sync.Once
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { c.joined <- struct{}{} })
	return c.Context.Done()
}

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for refresh")
	}
}

func TestGetOrRefreshSingleflight(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			inner := newCountingStore()
			store := newTestCache(t, inner)
			_ = store.Put("u", "s", Token{
				AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Minute),
				Scopes: []string{"read"},
			})
			joined := make(chan struct{}, 50)
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			wantErr := errors.New("provider denied refresh")
			var refreshes atomic.Int32
			refresh := func(_ context.Context, rt string) (string, string, time.Duration, error) {
				refreshes.Add(1)
				<-release
				if fail {
					return "", "", 0, wantErr
				}
				if rt != "refresh" {
					return "", "", 0, errors.New("wrong refresh token")
				}
				return "new", "rotated", time.Hour, nil
			}
			var wg sync.WaitGroup
			for range 50 {
				wg.Go(func() {
					ctx := &waitingContext{Context: context.Background(), joined: joined}
					tok, err := GetOrRefresh(ctx, store, "u", "s", refresh)
					if fail {
						if !errors.Is(err, wantErr) {
							t.Errorf("refresh error = %v", err)
						}
					} else if err != nil || tok.AccessToken != "new" || tok.RefreshToken != "rotated" {
						t.Errorf("refresh = %v, %v", tok, err)
					} else {
						tok.Scopes[0] = "private"
					}
				})
			}
			for range 50 {
				awaitSignal(t, joined)
			}
			close(release)
			wg.Wait()
			if refreshes.Load() != 1 {
				t.Fatalf("refreshes = %d, want 1", refreshes.Load())
			}
			wantPuts := 2
			if fail {
				wantPuts = 1
			}
			if inner.puts != wantPuts {
				t.Fatalf("Puts = %d, want %d", inner.puts, wantPuts)
			}
			_, err := GetOrRefresh(context.Background(), store, "u", "s", refresh)
			if fail {
				if !errors.Is(err, wantErr) || refreshes.Load() != 2 {
					t.Fatalf("retry = %v; refreshes = %d", err, refreshes.Load())
				}
			} else if err != nil || refreshes.Load() != 1 {
				t.Fatalf("fresh Get = %v; refreshes = %d", err, refreshes.Load())
			}
		})
	}
}

func TestGetOrRefreshCancellation(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner)
	_ = store.Put("u", "s", Token{
		AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute),
	})
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	refresh := func(ctx context.Context, _ string) (string, string, time.Duration, error) {
		close(started)
		select {
		case <-release:
			return "new", "", time.Hour, nil
		case <-ctx.Done():
			return "", "", 0, ctx.Err()
		}
	}
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := GetOrRefresh(leaderCtx, store, "u", "s", refresh)
		leaderDone <- err
	}()
	awaitSignal(t, started)
	joined := make(chan struct{}, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiterDone := make(chan error, 1)
	go func() {
		_, err := GetOrRefresh(&waitingContext{Context: ctx, joined: joined}, store, "u", "s", refresh)
		waiterDone <- err
	}()
	survivorDone := make(chan error, 1)
	go func() {
		tok, err := GetOrRefresh(&waitingContext{Context: context.Background(), joined: joined}, store, "u", "s", refresh)
		if err == nil && (tok.AccessToken != "new" || tok.RefreshToken != "refresh") {
			err = errors.New("incorrect refreshed token")
		}
		survivorDone <- err
	}()
	awaitSignal(t, joined)
	awaitSignal(t, joined)
	cancel()
	cancelLeader()
	for _, done := range []<-chan error{waiterDone, leaderDone} {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled caller = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation blocked on refresh")
		}
	}
	release <- struct{}{}
	select {
	case err := <-survivorDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not continue for surviving caller")
	}
}

func TestGetOrRefreshMissingAndCanceled(t *testing.T) {
	store := newCountingStore()
	if _, err := GetOrRefresh(context.Background(), store, "u", "s", nil); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("missing token = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetOrRefresh(ctx, store, "u", "s", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller = %v", err)
	}
	if store.gets != 1 {
		t.Fatal("canceled caller touched the store")
	}
}

func TestGetOrRefreshPutFailure(t *testing.T) {
	inner := newCountingStore()
	store := newTestCache(t, inner)
	_ = store.Put("u", "s", Token{
		AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute),
	})
	want := errors.New("database write failed")
	inner.putErr = want
	refreshes := 0
	refresh := func(context.Context, string) (string, string, time.Duration, error) {
		refreshes++
		return "new", "", time.Hour, nil
	}
	for range 2 {
		if _, err := GetOrRefresh(context.Background(), store, "u", "s", refresh); !errors.Is(err, want) {
			t.Fatalf("Put error = %v", err)
		}
		if len(store.entries) != 0 {
			t.Fatal("failed write cached refreshed credentials")
		}
	}
	if refreshes != 2 {
		t.Fatalf("refreshes = %d, want 2", refreshes)
	}
}

func TestGetOrRefreshIndependentPairsAndStores(t *testing.T) {
	blocked := newCountingStore()
	for _, service := range []string{"a", "b"} {
		_ = blocked.Put("u", service, Token{
			AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute),
		})
	}
	other := newCountingStore()
	_ = other.Put("u", "a", Token{
		AccessToken: "other", RefreshToken: "other-refresh", ExpiresAt: time.Now().Add(-time.Minute),
	})
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := GetOrRefresh(context.Background(), blocked, "u", "a", func(context.Context, string) (string, string, time.Duration, error) {
			close(started)
			<-release
			return "new", "", time.Hour, nil
		})
		done <- err
	}()
	awaitSignal(t, started)
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, tc := range []struct {
		store   Store
		service string
		want    string
	}{
		{blocked, "b", "new-b"},
		{other, "a", "new-other"},
	} {
		tok, err := GetOrRefresh(ctx, tc.store, "u", tc.service, func(context.Context, string) (string, string, time.Duration, error) {
			return tc.want, "", time.Hour, nil
		})
		if err != nil || tok.AccessToken != tc.want {
			t.Fatalf("independent refresh = %v, %v", tok, err)
		}
	}
}
