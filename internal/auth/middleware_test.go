package auth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE agent_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			allowed_services TEXT NOT NULL DEFAULT '["*"]',
			allowed_users TEXT NOT NULL DEFAULT '["*"]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMiddleware_ValidKey(t *testing.T) {
	t.Parallel()
	db := setupAuthTestDB(t)
	store := NewKeyStore(db)

	_, plaintext, err := store.Create(t.Context(), "test-agent", []string{"*"}, []string{"*"})
	if err != nil {
		t.Fatal(err)
	}

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := AgentKeyFromContext(r.Context())
		if !ok {
			t.Fatal("expected key in context")
		}
		if key.Name != "test-agent" {
			t.Fatalf("name = %s", key.Name)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-AgentGate-Key", plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestMiddleware_InvalidKey(t *testing.T) {
	t.Parallel()
	db := setupAuthTestDB(t)
	store := NewKeyStore(db)

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-AgentGate-Key", "ag_live_invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_MissingKey(t *testing.T) {
	t.Parallel()
	db := setupAuthTestDB(t)
	store := NewKeyStore(db)

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_RevokedKey(t *testing.T) {
	t.Parallel()
	db := setupAuthTestDB(t)
	store := NewKeyStore(db)

	key, plaintext, _ := store.Create(t.Context(), "revokable", []string{"*"}, []string{"*"})
	_ = store.Revoke(t.Context(), key.ID)

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-AgentGate-Key", plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_BearerAuth(t *testing.T) {
	t.Parallel()
	db := setupAuthTestDB(t)
	store := NewKeyStore(db)

	_, plaintext, _ := store.Create(t.Context(), "bearer-agent", []string{"*"}, []string{"*"})

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
