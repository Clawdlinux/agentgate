package auth

import (
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrKeyNotFound = errors.New("auth: key not found")
	ErrKeyRevoked  = errors.New("auth: key revoked")
)

// AgentKey represents an API key for an agent.
type AgentKey struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	KeyHash         string     `json:"-"`
	AllowedServices []string   `json:"allowed_services"`
	AllowedUsers    []string   `json:"allowed_users"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

// CanAccessService checks if the key is allowed to access the given service.
func (k *AgentKey) CanAccessService(service string) bool {
	for _, s := range k.AllowedServices {
		if s == "*" || s == service {
			return true
		}
	}
	return false
}

// CanAccessUser checks if the key is allowed to act on behalf of the given user.
func (k *AgentKey) CanAccessUser(userID string) bool {
	for _, u := range k.AllowedUsers {
		if u == "*" || u == userID {
			return true
		}
	}
	return false
}

// KeyStore manages agent API keys in SQLite.
type KeyStore struct {
	db              keyDatabase
	pepper          []byte
	compareLegacy   func([]byte, []byte) error
	now             func() time.Time
	cacheMu         sync.Mutex
	cacheGeneration uint64
	cache           map[[sha256.Size]byte]*list.Element
	lru             *list.List
	cacheTTL        time.Duration
	cacheSize       int
}

type keyDatabase interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type cachedKey struct {
	digest    [sha256.Size]byte
	key       AgentKey
	expiresAt time.Time
}

type KeyStoreOption func(*KeyStore) error

// WithCacheTTL sets the verified-key lifetime. Zero disables caching.
func WithCacheTTL(ttl time.Duration) KeyStoreOption {
	return func(s *KeyStore) error {
		if ttl < 0 {
			return errors.New("auth: cache TTL must not be negative")
		}
		s.cacheTTL = ttl
		return nil
	}
}

// WithCacheSize sets the maximum verified-key count. Zero disables caching.
func WithCacheSize(size int) KeyStoreOption {
	return func(s *KeyStore) error {
		if size < 0 {
			return errors.New("auth: cache size must not be negative")
		}
		s.cacheSize = size
		return nil
	}
}

// DeriveKeyPepper derives an API-key HMAC key from the vault's 32-byte master key.
func DeriveKeyPepper(masterKey []byte) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("auth: vault key must contain exactly 32 bytes")
	}
	pepper := make([]byte, sha256.Size)
	if _, err := io.ReadFull(hkdf.New(sha256.New, masterKey, nil, []byte("agentgate api-key pepper v1")), pepper); err != nil {
		return nil, fmt.Errorf("auth: derive pepper: %w", err)
	}
	return pepper, nil
}

// NewKeyStore creates a new key store backed by SQLite.
func NewKeyStore(db *sql.DB, pepper []byte, options ...KeyStoreOption) (*KeyStore, error) {
	if len(pepper) != sha256.Size {
		return nil, errors.New("auth: pepper must contain exactly 32 bytes")
	}
	s := &KeyStore{
		db: db, pepper: append([]byte(nil), pepper...),
		compareLegacy: bcrypt.CompareHashAndPassword,
		now:           time.Now,
		cache:         make(map[[sha256.Size]byte]*list.Element),
		lru:           list.New(),
		cacheTTL:      30 * time.Second,
		cacheSize:     1024,
	}
	for _, option := range options {
		if err := option(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// GenerateKey returns a plaintext key and its public lookup ID.
// Both random components use lowercase hex: agk_<16-byte ID>_<32-byte secret>.
func GenerateKey() (plaintext string, keyID string, err error) {
	var raw [48]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", fmt.Errorf("auth: generate key: %w", err)
	}
	keyID = hex.EncodeToString(raw[:16])
	return "agk_" + keyID + "_" + hex.EncodeToString(raw[16:]), keyID, nil
}

// Create creates a new agent key and stores it.
// Returns the AgentKey and the plaintext key (shown once).
func (s *KeyStore) Create(ctx context.Context, name string, services, users []string) (*AgentKey, string, error) {
	plaintext, id, err := GenerateKey()
	if err != nil {
		return nil, "", err
	}

	_, secret, _ := parseKey(plaintext)
	mac := s.keyHMAC(secret[:])
	servicesJSON, _ := json.Marshal(services)
	usersJSON, _ := json.Marshal(users)

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO agent_keys (id, name, key_hash, key_id, key_hmac, allowed_services, allowed_users) VALUES (?, ?, '', ?, ?, ?, ?)",
		id, name, id, mac, string(servicesJSON), string(usersJSON),
	)
	if err != nil {
		return nil, "", fmt.Errorf("auth: create key: %w", err)
	}

	key := &AgentKey{
		ID:              id,
		Name:            name,
		AllowedServices: services,
		AllowedUsers:    users,
		CreatedAt:       time.Now(),
	}
	return key, plaintext, nil
}

// Validate verifies indexed HMAC keys or strictly formatted legacy bcrypt keys.
// Returns the matching AgentKey or an error.
func (s *KeyStore) Validate(ctx context.Context, plaintext string) (*AgentKey, error) {
	id, secret, modern := parseKey(plaintext)
	legacy := !modern && validLegacyKey(plaintext)
	if !modern && !legacy {
		return nil, ErrKeyNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(plaintext))
	key, generation := s.cached(digest)
	if key != nil {
		if key.ID == receiptspec.AdvisorAgentKeyID {
			return nil, ErrKeyNotFound
		}
		return key, nil
	}
	var err error
	if modern {
		key, err = s.validateHMAC(ctx, id, secret[:])
	} else {
		key, err = s.validateLegacy(ctx, plaintext)
	}
	if err != nil {
		return nil, err
	}
	if key.ID == receiptspec.AdvisorAgentKeyID {
		return nil, ErrKeyNotFound
	}
	s.cacheKey(digest, key, generation)
	return key, nil
}

func (s *KeyStore) validateHMAC(ctx context.Context, id string, secret []byte) (*AgentKey, error) {
	var key AgentKey
	var servicesJSON, usersJSON string
	var revokedAt sql.NullTime
	var storedMAC []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, key_hmac, allowed_services, allowed_users, created_at, revoked_at FROM agent_keys WHERE key_id = ?", id,
	).Scan(&key.ID, &key.Name, &storedMAC, &servicesJSON, &usersJSON, &key.CreatedAt, &revokedAt)
	// Missing IDs still perform one HMAC and a same-length comparison.
	if err != nil {
		storedMAC = make([]byte, sha256.Size)
	}
	matches := hmac.Equal(s.keyHMAC(secret), storedMAC)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: lookup key: %w", err)
	}
	if !matches {
		return nil, ErrKeyNotFound
	}
	if revokedAt.Valid {
		return nil, ErrKeyRevoked
	}
	if err := decodePermissions(&key, servicesJSON, usersJSON); err != nil {
		return nil, err
	}
	return &key, nil
}

var (
	warnedLegacyKeys      sync.Map
	legacyValidationSlots = make(chan struct{}, 2)
	legacySaturationLogAt atomic.Int64
)

func (s *KeyStore) validateLegacy(ctx context.Context, plaintext string) (*AgentKey, error) {
	lookup := s.keyHMAC([]byte(plaintext))
	var key AgentKey
	var servicesJSON, usersJSON string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, allowed_services, allowed_users, created_at FROM agent_keys WHERE legacy_lookup = ? AND key_id IS NULL AND revoked_at IS NULL",
		lookup,
	).Scan(&key.ID, &key.Name, &servicesJSON, &usersJSON, &key.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		found, err := s.scanLegacy(ctx, plaintext)
		if err != nil {
			return nil, err
		}
		result, err := s.db.ExecContext(ctx,
			"UPDATE agent_keys SET legacy_lookup = ? WHERE id = ? AND revoked_at IS NULL", lookup, found.ID)
		if err != nil {
			return nil, fmt.Errorf("auth: upgrade legacy key: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("auth: upgrade legacy result: %w", err)
		}
		if affected == 0 {
			return nil, ErrKeyNotFound
		}
		key = *found
	} else if err != nil {
		return nil, fmt.Errorf("auth: lookup legacy key: %w", err)
	} else if err := decodePermissions(&key, servicesJSON, usersJSON); err != nil {
		return nil, err
	}
	if _, loaded := warnedLegacyKeys.LoadOrStore(key.ID, struct{}{}); !loaded {
		slog.Warn("deprecated legacy API key; replace with an agk_ key", "key_id", key.ID)
	}
	return &key, nil
}

type legacyCandidate struct {
	key          AgentKey
	servicesJSON string
	usersJSON    string
}

func (s *KeyStore) legacyCandidates(ctx context.Context) ([]legacyCandidate, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, key_hash, allowed_services, allowed_users, created_at FROM agent_keys WHERE key_id IS NULL AND legacy_lookup IS NULL AND revoked_at IS NULL",
	)
	if err != nil {
		return nil, fmt.Errorf("auth: query keys: %w", err)
	}
	defer rows.Close()

	var candidates []legacyCandidate
	for rows.Next() {
		var candidate legacyCandidate
		key := &candidate.key
		if err := rows.Scan(&key.ID, &key.Name, &key.KeyHash, &candidate.servicesJSON, &candidate.usersJSON, &key.CreatedAt); err != nil {
			return nil, fmt.Errorf("auth: scan legacy key: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: read legacy keys: %w", err)
	}
	return candidates, nil
}

func (s *KeyStore) scanLegacy(ctx context.Context, plaintext string) (*AgentKey, error) {
	select {
	case legacyValidationSlots <- struct{}{}:
		defer func() { <-legacyValidationSlots }()
	default:
		now := time.Now().UnixNano()
		next := legacySaturationLogAt.Load()
		if now >= next && legacySaturationLogAt.CompareAndSwap(next, now+int64(time.Minute)) {
			slog.Debug("legacy API key fallback capacity reached")
		}
		return nil, ErrKeyNotFound
	}

	// Release the database connection before bcrypt, so revocation can proceed.
	candidates, err := s.legacyCandidates(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := candidate.key
		if s.compareLegacy([]byte(key.KeyHash), []byte(plaintext)) != nil {
			continue
		}
		if err := decodePermissions(&key, candidate.servicesJSON, candidate.usersJSON); err != nil {
			return nil, err
		}
		key.KeyHash = ""
		return &key, nil
	}
	return nil, ErrKeyNotFound
}

func decodePermissions(key *AgentKey, services, users string) error {
	if err := json.Unmarshal([]byte(services), &key.AllowedServices); err != nil {
		return fmt.Errorf("auth: decode services: %w", err)
	}
	if err := json.Unmarshal([]byte(users), &key.AllowedUsers); err != nil {
		return fmt.Errorf("auth: decode users: %w", err)
	}
	return nil
}

func (s *KeyStore) keyHMAC(secret []byte) []byte {
	mac := hmac.New(sha256.New, s.pepper)
	mac.Write(secret)
	return mac.Sum(nil)
}

func parseKey(plaintext string) (string, [32]byte, bool) {
	var secret [32]byte
	if len(plaintext) != 101 || !strings.HasPrefix(plaintext, "agk_") || plaintext[36] != '_' ||
		!lowerHex(plaintext[4:36]) || !lowerHex(plaintext[37:]) {
		return "", secret, false
	}
	_, err := hex.Decode(secret[:], []byte(plaintext[37:]))
	return plaintext[4:36], secret, err == nil
}

func validLegacyKey(plaintext string) bool {
	return len(plaintext) == 72 && strings.HasPrefix(plaintext, "ag_live_") && lowerHex(plaintext[8:])
}

func lowerHex(value string) bool {
	for i := range value {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func cloneKey(key *AgentKey) *AgentKey {
	clone := *key
	clone.AllowedServices = append([]string(nil), key.AllowedServices...)
	clone.AllowedUsers = append([]string(nil), key.AllowedUsers...)
	if key.RevokedAt != nil {
		revoked := *key.RevokedAt
		clone.RevokedAt = &revoked
	}
	return &clone
}

func (s *KeyStore) cached(digest [sha256.Size]byte) (*AgentKey, uint64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry := s.cache[digest]
	if entry == nil {
		return nil, s.cacheGeneration
	}
	cached := entry.Value.(*cachedKey)
	if !s.now().Before(cached.expiresAt) {
		s.lru.Remove(entry)
		delete(s.cache, digest)
		return nil, s.cacheGeneration
	}
	s.lru.MoveToFront(entry)
	return cloneKey(&cached.key), s.cacheGeneration
}

func (s *KeyStore) cacheKey(digest [sha256.Size]byte, key *AgentKey, generation uint64) {
	if s.cacheSize == 0 || s.cacheTTL == 0 {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if generation != s.cacheGeneration {
		return
	}
	if entry := s.cache[digest]; entry != nil {
		s.lru.Remove(entry)
	}
	s.cache[digest] = s.lru.PushFront(&cachedKey{
		digest: digest, key: *cloneKey(key), expiresAt: s.now().Add(s.cacheTTL),
	})
	if s.lru.Len() > s.cacheSize {
		oldest := s.lru.Back()
		delete(s.cache, oldest.Value.(*cachedKey).digest)
		s.lru.Remove(oldest)
	}
}

// Count returns the number of agent keys on record, including revoked
// ones. Used by the composition root to decide whether to bootstrap a
// first agent key on an empty database.
func (s *KeyStore) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_keys").Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count: %w", err)
	}
	return n, nil
}

// Revoke marks a key as revoked.
func (s *KeyStore) Revoke(ctx context.Context, keyID string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE agent_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND revoked_at IS NULL",
		keyID,
	)
	if err != nil {
		return fmt.Errorf("auth: revoke: %w", err)
	}
	s.cacheMu.Lock()
	s.cacheGeneration++
	for digest, entry := range s.cache {
		if entry.Value.(*cachedKey).key.ID == keyID {
			s.lru.Remove(entry)
			delete(s.cache, digest)
		}
	}
	s.cacheMu.Unlock()
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: revoke result: %w", err)
	}
	if rows == 0 {
		return ErrKeyNotFound
	}
	return nil
}
