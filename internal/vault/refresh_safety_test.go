package vault

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func safetyStore(t *testing.T, kind string) (Store, Store) {
	t.Helper()
	if kind == "memory" {
		store, err := NewMemoryStore(testKey())
		if err != nil {
			t.Fatal(err)
		}
		return store, store
	}
	db := setupTestDB(t)
	db.SetMaxOpenConns(1)
	key := testKey()
	store, err := NewSQLiteStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	external, err := NewSQLiteStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	if kind == "cached" {
		return newTestCache(t, store), external
	}
	return store, external
}

func TestStoreCompareAndPut(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite", "cached"} {
		t.Run(kind, func(t *testing.T) {
			store, _ := safetyStore(t, kind)
			old := Token{AccessToken: "old", RefreshToken: "original"}
			newToken := Token{AccessToken: "new", RefreshToken: "rotated", Scopes: []string{"new-scope"}}
			if ok, err := store.CompareAndPut("u", "s", "original", newToken); err != nil || ok {
				t.Fatalf("missing CompareAndPut = %v, %v", ok, err)
			}
			if err := store.Put("u", "s", old); err != nil {
				t.Fatal(err)
			}
			if ok, err := store.CompareAndPut("u", "s", "wrong", newToken); err != nil || ok {
				t.Fatalf("mismatched CompareAndPut = %v, %v", ok, err)
			}
			if ok, err := store.CompareAndPut("u", "s", "original", newToken); err != nil || !ok {
				t.Fatalf("matching CompareAndPut = %v, %v", ok, err)
			}
			got, err := store.Get("u", "s")
			if err != nil || got.AccessToken != "new" || got.Scopes[0] != "new-scope" {
				t.Fatalf("Get = %v, %v", got, err)
			}
			if ok, err := store.CompareAndPut("u", "s", "original", old); err != nil || ok {
				t.Fatalf("stale CompareAndPut = %v, %v", ok, err)
			}
			if err := store.Delete("u", "s"); err != nil {
				t.Fatal(err)
			}
			if ok, err := store.CompareAndPut("u", "s", "rotated", old); err != nil || ok {
				t.Fatalf("deleted CompareAndPut = %v, %v", ok, err)
			}
			if err := store.Put("u", "s", Token{AccessToken: "no-refresh"}); err != nil {
				t.Fatal(err)
			}
			if ok, err := store.CompareAndPut("u", "s", "", newToken); err != nil || !ok {
				t.Fatalf("empty-refresh CompareAndPut = %v, %v", ok, err)
			}
		})
	}
}

func TestRefreshCannotOverwriteDeleteOrRelink(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite", "cached"} {
		for _, operation := range []string{"delete", "relink", "expired-relink"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				store, external := safetyStore(t, kind)
				if err := store.Put("u", "s", Token{
					AccessToken: "old", RefreshToken: "old-refresh",
					ExpiresAt: time.Now().Add(time.Minute), Scopes: []string{"old-scope"},
				}); err != nil {
					t.Fatal(err)
				}
				started, release := make(chan struct{}), make(chan struct{})
				done := make(chan struct{})
				var got Token
				var refreshErr error
				go func() {
					defer close(done)
					got, refreshErr = GetOrRefresh(context.Background(), store, "u", "s",
						func(context.Context, string) (string, string, time.Duration, error) {
							close(started)
							<-release
							return "stale-access", "stale-refresh", time.Hour, nil
						})
				}()
				awaitSignal(t, started)
				if operation == "delete" {
					if err := external.Delete("u", "s"); err != nil {
						t.Error(err)
					}
				} else {
					expiry := time.Now().Add(time.Hour)
					if operation == "expired-relink" {
						expiry = time.Now().Add(-time.Minute)
					}
					if err := external.Put("u", "s", Token{
						AccessToken: "relinked-access", RefreshToken: "relinked-refresh",
						ExpiresAt: expiry, Scopes: []string{"relinked-scope"},
					}); err != nil {
						t.Error(err)
					}
				}
				close(release)
				awaitSignal(t, done)
				current, err := external.Get("u", "s")
				if operation == "delete" {
					if !errors.Is(err, ErrTokenNotFound) || !errors.Is(refreshErr, ErrTokenNotFound) {
						t.Fatalf("deleted Get = %v; refresh = %v", err, refreshErr)
					}
				} else {
					if err != nil || current.AccessToken != "relinked-access" || current.Scopes[0] != "relinked-scope" {
						t.Fatalf("relinked Get = %v, %v", current, err)
					}
					if operation == "expired-relink" {
						if refreshErr == nil {
							t.Fatal("expired replacement was returned")
						}
					} else if refreshErr != nil || got.AccessToken != "relinked-access" || got.Scopes[0] != "relinked-scope" {
						t.Fatalf("refresh returned stale grant: %v, %v", got, refreshErr)
					}
				}
			})
		}
	}
}

func TestRefreshTimeoutAllowsAnotherFlight(t *testing.T) {
	store := newCountingStore()
	_ = store.Put("u", "s", Token{
		AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute),
	})
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	refresh := func(ctx context.Context, _ string) (string, string, time.Duration, error) {
		if ctx.Value(contextKey{}) != "kept" {
			t.Error("shared refresh lost context values")
		}
		<-ctx.Done()
		return "", "", 0, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := getOrRefresh(ctx, store, "u", "s", refresh, 20*time.Millisecond)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shared refresh exceeded its timeout")
	}
	tok, err := GetOrRefresh(context.Background(), store, "u", "s",
		func(context.Context, string) (string, string, time.Duration, error) {
			return "recovered", "rotated", time.Hour, nil
		})
	if err != nil || tok.AccessToken != "recovered" {
		t.Fatalf("next flight = %v, %v", tok, err)
	}
}

func TestRotatedRefreshRetriesPersistence(t *testing.T) {
	for _, failures := range []int{1, 2, 3} {
		t.Run(string(rune('0'+failures)), func(t *testing.T) {
			inner := newCountingStore()
			store := newTestCache(t, inner)
			_ = store.Put("u", "s", Token{
				AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute),
			})
			for range failures {
				inner.casErrors = append(inner.casErrors, errors.New("secret-new secret-rotated"))
			}
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			tok, err := GetOrRefresh(context.Background(), store, "u", "s",
				func(context.Context, string) (string, string, time.Duration, error) {
					return "secret-new", "secret-rotated", time.Hour, nil
				})
			if err != nil || tok.AccessToken != "secret-new" {
				t.Fatalf("refreshed result = %v, %v", tok, err)
			}
			wantAttempts := failures + 1
			if wantAttempts > 3 {
				wantAttempts = 3
			}
			if inner.compares != wantAttempts {
				t.Fatalf("write attempts = %d, want %d", inner.compares, wantAttempts)
			}
			if failures == 3 {
				if logs.Len() == 0 || len(store.entries) != 0 {
					t.Fatal("permanent write failure wasn't logged or invalidated")
				}
				if strings.Contains(logs.String(), "secret-new") || strings.Contains(logs.String(), "secret-rotated") {
					t.Fatal("tokens leaked into persistence logs")
				}
			} else if stored, err := store.Get("u", "s"); err != nil || stored.RefreshToken != "secret-rotated" {
				t.Fatalf("persisted rotation = %v, %v", stored, err)
			}
		})
	}
}
