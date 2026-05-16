package audit

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Entry represents one audit log entry.
type Entry struct {
	AgentKeyID string
	Service    string
	Action     string
	UserID     string
	StatusCode int
	LatencyMs  int64
	Error      string
}

// Logger writes audit entries to SQLite.
type Logger struct {
	db     *sql.DB
	ch     chan Entry
	logger *slog.Logger
}

// NewLogger creates an async audit logger.
// Entries are buffered and written in the background.
func NewLogger(db *sql.DB, logger *slog.Logger) *Logger {
	if logger == nil {
		logger = slog.Default()
	}
	l := &Logger{
		db:     db,
		ch:     make(chan Entry, 1000),
		logger: logger,
	}
	go l.drain()
	return l
}

// Log queues an audit entry for writing.
func (l *Logger) Log(e Entry) {
	select {
	case l.ch <- e:
	default:
		l.logger.Warn("audit: buffer full, dropping entry",
			"service", e.Service, "action", e.Action)
	}
}

func (l *Logger) drain() {
	for e := range l.ch {
		_, err := l.db.Exec(`
			INSERT INTO audit_log (agent_key_id, service, action, user_id, status_code, latency_ms, error)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, e.AgentKeyID, e.Service, e.Action, e.UserID, e.StatusCode, e.LatencyMs, e.Error)
		if err != nil {
			l.logger.Error("audit: write failed", "error", err)
		}
	}
}

// Close shuts down the audit logger, flushing remaining entries.
func (l *Logger) Close() {
	close(l.ch)
}

// Query returns recent audit entries. If limit <= 0, defaults to 100.
func Query(db *sql.DB, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT timestamp, agent_key_id, service, action, user_id, status_code, latency_ms, error
		FROM audit_log ORDER BY timestamp DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit.Query: %w", err)
	}
	defer rows.Close()

	var entries []map[string]interface{}
	for rows.Next() {
		var ts time.Time
		var keyID, svc, action, userID string
		var status int
		var latency int64
		var errStr sql.NullString

		if err := rows.Scan(&ts, &keyID, &svc, &action, &userID, &status, &latency, &errStr); err != nil {
			continue
		}
		entry := map[string]interface{}{
			"timestamp":    ts,
			"agent_key_id": keyID,
			"service":      svc,
			"action":       action,
			"user_id":      userID,
			"status_code":  status,
			"latency_ms":   latency,
		}
		if errStr.Valid {
			entry["error"] = errStr.String
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
