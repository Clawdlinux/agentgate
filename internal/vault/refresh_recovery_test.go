package vault

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuccessfulRotationAfterDeadline(t *testing.T) {
	for _, failures := range []int{0, 2, 3} {
		t.Run(string(rune('0'+failures)), func(t *testing.T) {
			inner := newCountingStore()
			store := newTestCache(t, inner)
			_ = store.Put("u", "s", Token{
				AccessToken: "old", RefreshToken: "original", ExpiresAt: time.Now().Add(-time.Minute),
			})
			for range failures {
				inner.casErrors = append(inner.casErrors, errors.New("write failed"))
			}
			tok, err := getOrRefresh(context.Background(), store, "u", "s",
				func(ctx context.Context, _ string) (string, string, time.Duration, error) {
					<-ctx.Done()
					return "new", "rotated", time.Hour, nil
				}, 10*time.Millisecond)
			if err != nil || tok.AccessToken != "new" || tok.RefreshToken != "rotated" {
				t.Fatalf("late successful exchange = %v, %v", tok, err)
			}
			wantAttempts := failures + 1
			if wantAttempts > 3 {
				wantAttempts = 3
			}
			if inner.compares != wantAttempts {
				t.Fatalf("write attempts = %d, want %d", inner.compares, wantAttempts)
			}
			if failures < 3 {
				current, err := store.Get("u", "s")
				if err != nil || current.AccessToken != "new" || current.RefreshToken != "rotated" {
					t.Fatalf("saved rotation = %v, %v", current, err)
				}
			} else if len(store.entries) != 0 {
				t.Fatal("failed persistence cached a rotated token")
			}
		})
	}
}

func TestRefreshPanicReleasesWaiters(t *testing.T) {
	store := newCountingStore()
	_ = store.Put("u", "s", Token{
		AccessToken: "old", RefreshToken: "original", ExpiresAt: time.Now().Add(-time.Minute),
	})
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	joined, release := make(chan struct{}, 50), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var refreshes atomic.Int32
	refresh := func(context.Context, string) (string, string, time.Duration, error) {
		refreshes.Add(1)
		<-release
		panic("secret-refresh-token")
	}
	results := make(chan error, 50)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			ctx := &waitingContext{Context: context.Background(), joined: joined}
			_, err := GetOrRefresh(ctx, store, "u", "s", refresh)
			results <- err
		})
	}
	for range 50 {
		awaitSignal(t, joined)
	}
	close(release)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	awaitSignal(t, done)
	for range 50 {
		if err := <-results; err == nil || err.Error() != "vault: refresh panicked" {
			t.Fatalf("panic error = %v", err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("panicking refreshes = %d, want 1", refreshes.Load())
	}
	if logs.Len() == 0 || strings.Contains(logs.String(), "secret-refresh-token") {
		t.Fatal("panic logging was missing or leaked the panic value")
	}
	tok, err := GetOrRefresh(context.Background(), store, "u", "s",
		func(context.Context, string) (string, string, time.Duration, error) {
			refreshes.Add(1)
			return "recovered", "rotated", time.Hour, nil
		})
	if err != nil || tok.AccessToken != "recovered" || refreshes.Load() != 2 {
		t.Fatalf("next refresh = %v, %v; exchanges = %d", tok, err, refreshes.Load())
	}
}

type panickingFlightStore struct {
	Store
	point    string
	gets     atomic.Int32
	panicked atomic.Bool
}

func (s *panickingFlightStore) Get(user, service string) (Token, error) {
	if s.gets.Add(1) == 2 && s.point == "get" {
		panic("secret-store-token")
	}
	return s.Store.Get(user, service)
}

func (s *panickingFlightStore) CompareAndPut(user, service, expected string, tok Token) (bool, error) {
	if s.point == "compare" && !s.panicked.Swap(true) {
		panic("secret-store-token")
	}
	return s.Store.CompareAndPut(user, service, expected, tok)
}

func TestRefreshStorePanicReleasesFlight(t *testing.T) {
	for _, point := range []string{"get", "compare"} {
		t.Run(point, func(t *testing.T) {
			store := &panickingFlightStore{Store: newCountingStore(), point: point}
			_ = store.Put("u", "s", Token{
				AccessToken: "old", RefreshToken: "original", ExpiresAt: time.Now().Add(-time.Minute),
			})
			refresh := func(context.Context, string) (string, string, time.Duration, error) {
				return "recovered", "rotated", time.Hour, nil
			}
			if _, err := GetOrRefresh(context.Background(), store, "u", "s", refresh); err == nil || err.Error() != "vault: refresh panicked" {
				t.Fatalf("store panic error = %v", err)
			}
			tok, err := GetOrRefresh(context.Background(), store, "u", "s", refresh)
			if err != nil || tok.AccessToken != "recovered" {
				t.Fatalf("next refresh = %v, %v", tok, err)
			}
		})
	}
}
