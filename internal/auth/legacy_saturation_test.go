package auth

import (
	"bytes"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLegacySaturationLoggingIsRateLimitedAndContainsNoKey(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(logger) })
	next := legacySaturationLogAt.Swap(0)
	t.Cleanup(func() { legacySaturationLogAt.Store(next) })
	for i := 0; i < 2; i++ {
		legacyValidationSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < 2; i++ {
			<-legacyValidationSlots
		}
	}()
	store := newTestKeyStore(t, testDB(t))
	database := &countedDB{DB: store.db.(*sql.DB)}
	store.db = database
	calls := countBcrypt(store)
	key := "ag_live_" + strings.Repeat("f", 64)
	for i := 0; i < 2; i++ {
		if _, err := store.Validate(t.Context(), key); !errors.Is(err, ErrKeyNotFound) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 || database.lookups.Load() != 2 || database.scans.Load() != 0 {
		t.Fatal("saturated validation did more than indexed lookups")
	}
	if strings.Count(logs.String(), "capacity reached") != 1 || !strings.Contains(logs.String(), "level=DEBUG") ||
		strings.Contains(logs.String(), "ag_live_") {
		t.Fatalf("unexpected saturation logging: %s", logs.String())
	}
}
