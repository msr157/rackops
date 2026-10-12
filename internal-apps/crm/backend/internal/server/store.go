// Package server implements the RackOps CRM: SQLite-backed store, public
// ingest API for customer instances, JWT-guarded admin API, and the embedded
// dashboard.
package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database and the persisted session secret.
type Store struct {
	db     *sql.DB
	secret []byte
}

// OpenStore opens (creating if needed) the CRM database inside dataDir and
// runs migrations. It also loads or creates the JWT session secret.
func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := filepath.Join(dataDir, "crm.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// modernc.org/sqlite serializes writes; a single connection avoids
	// SQLITE_BUSY under concurrent handlers.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	secret, err := loadOrCreateSecret(filepath.Join(dataDir, "session.secret"))
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, secret: secret}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// SessionSecret returns the JWT signing secret (random 32 bytes persisted in
// the data dir — same approach as RackOps Lite's SessionSecret).
func (s *Store) SessionSecret() []byte { return s.secret }

func loadOrCreateSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate session secret: %w", err)
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, fmt.Errorf("persist session secret: %w", err)
	}
	return secret, nil
}

func migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS installs (
	id            TEXT PRIMARY KEY,
	name          TEXT NOT NULL DEFAULT '',
	version       TEXT NOT NULL DEFAULT '',
	os            TEXT NOT NULL DEFAULT '',
	arch          TEXT NOT NULL DEFAULT '',
	first_seen    TIMESTAMP NOT NULL,
	last_seen     TIMESTAMP NOT NULL,
	last_snapshot JSON
);
CREATE TABLE IF NOT EXISTS leads (
 install_id TEXT NOT NULL,
 id TEXT NOT NULL,
 email TEXT NOT NULL,
 name TEXT NOT NULL,
 industry TEXT NOT NULL,
 notes TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'new',
 received_at TEXT NOT NULL,
 PRIMARY KEY (install_id, id)
);
CREATE TABLE IF NOT EXISTS snapshots (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	install_id TEXT NOT NULL,
	at         TIMESTAMP NOT NULL,
	data       JSON NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snapshots_install ON snapshots(install_id, at);
CREATE TABLE IF NOT EXISTS tickets (
	id               TEXT NOT NULL,
	install_id       TEXT NOT NULL,
	subject          TEXT NOT NULL DEFAULT '',
	category         TEXT NOT NULL DEFAULT 'general',
	severity         TEXT NOT NULL DEFAULT 'normal',
	status           TEXT NOT NULL DEFAULT 'open',
	email            TEXT NOT NULL DEFAULT '',
	created_at       TIMESTAMP NOT NULL,
	updated_at       TIMESTAMP NOT NULL,
	poll_secret_hash TEXT NOT NULL,
	PRIMARY KEY (id, install_id)
);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status, updated_at);
CREATE TABLE IF NOT EXISTS ticket_messages (
	id         TEXT NOT NULL,
	ticket_id  TEXT NOT NULL,
	install_id TEXT NOT NULL,
	author     TEXT NOT NULL CHECK (author IN ('user','agent','note')),
	name       TEXT NOT NULL DEFAULT '',
	body       TEXT NOT NULL DEFAULT '',
	at         TIMESTAMP NOT NULL,
	PRIMARY KEY (id, ticket_id, install_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_ticket ON ticket_messages(ticket_id, install_id, at);
CREATE TABLE IF NOT EXISTS ticket_revisions (
 ticket_id TEXT NOT NULL, install_id TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(ticket_id, install_id)
);
CREATE TABLE IF NOT EXISTS ticket_resolutions (
 ticket_id TEXT NOT NULL, install_id TEXT NOT NULL, action_id TEXT NOT NULL,
 expected_revision INTEGER NOT NULL, applied INTEGER NOT NULL,
 PRIMARY KEY(ticket_id, install_id, action_id)
);
CREATE TRIGGER IF NOT EXISTS ticket_revision_message AFTER INSERT ON ticket_messages
WHEN NEW.author != 'note'
BEGIN
 INSERT INTO ticket_revisions(ticket_id, install_id, revision) VALUES(NEW.ticket_id, NEW.install_id, 1)
 ON CONFLICT(ticket_id, install_id) DO UPDATE SET revision = revision + 1;
END;
CREATE TRIGGER IF NOT EXISTS ticket_revision_status AFTER UPDATE OF status ON tickets
WHEN OLD.status != NEW.status
BEGIN
 INSERT INTO ticket_revisions(ticket_id, install_id, revision) VALUES(NEW.id, NEW.install_id, 1)
 ON CONFLICT(ticket_id, install_id) DO UPDATE SET revision = revision + 1;
END;
CREATE TABLE IF NOT EXISTS ticket_message_delivery (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 message_id TEXT NOT NULL,
 ticket_id TEXT NOT NULL,
 install_id TEXT NOT NULL,
 UNIQUE(message_id, ticket_id, install_id)
);
CREATE INDEX IF NOT EXISTS idx_message_delivery_ticket ON ticket_message_delivery(ticket_id, install_id, sequence);
INSERT OR IGNORE INTO ticket_message_delivery(message_id, ticket_id, install_id)
 SELECT id, ticket_id, install_id FROM ticket_messages ORDER BY rowid;
CREATE TRIGGER IF NOT EXISTS ticket_message_delivery_insert AFTER INSERT ON ticket_messages
BEGIN
 INSERT INTO ticket_message_delivery(message_id, ticket_id, install_id)
 VALUES (NEW.id, NEW.ticket_id, NEW.install_id);
END;
CREATE TABLE IF NOT EXISTS reset_emails (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	install_id    TEXT NOT NULL,
	email         TEXT NOT NULL,
	instance_name TEXT NOT NULL DEFAULT '',
	at            TIMESTAMP NOT NULL,
	status        TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

// ---- installs ----

// UpsertInstall records or refreshes an install and, when usage is non-nil,
// stores a snapshot row and updates the cached last_snapshot.
func (s *Store) UpsertInstall(id, name, version string, usage *UsageSnapshot, now time.Time) error {
	osName, arch := "", ""
	var snapJSON []byte
	if usage != nil {
		osName, arch = usage.OS, usage.Arch
		if usage.Version != "" {
			version = usage.Version
		}
		var err error
		snapJSON, err = json.Marshal(usage)
		if err != nil {
			return fmt.Errorf("marshal snapshot: %w", err)
		}
	}
	_, err := s.db.Exec(`
INSERT INTO installs (id, name, version, os, arch, first_seen, last_seen, last_snapshot)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	name          = CASE WHEN excluded.name    != '' THEN excluded.name    ELSE installs.name    END,
	version       = CASE WHEN excluded.version != '' THEN excluded.version ELSE installs.version END,
	os            = CASE WHEN excluded.os      != '' THEN excluded.os      ELSE installs.os      END,
	arch          = CASE WHEN excluded.arch    != '' THEN excluded.arch    ELSE installs.arch    END,
	last_seen     = excluded.last_seen,
	last_snapshot = COALESCE(excluded.last_snapshot, installs.last_snapshot)`,
		id, name, version, osName, arch, now.UTC(), now.UTC(), nullableJSON(snapJSON))
	if err != nil {
		return fmt.Errorf("upsert install: %w", err)
	}
	if usage != nil {
		if _, err := s.db.Exec(`INSERT INTO snapshots (install_id, at, data) VALUES (?, ?, ?)`,
			id, now.UTC(), string(snapJSON)); err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
	}
	return nil
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// ListInstalls returns all installs with their last snapshot summary and
// open-ticket counts, most recently seen first.
func (s *Store) ListInstalls() ([]*Install, error) {
	rows, err := s.db.Query(`
SELECT i.id, i.name, i.version, i.os, i.arch, i.first_seen, i.last_seen, i.last_snapshot,
	(SELECT COUNT(*) FROM tickets t WHERE t.install_id = i.id AND t.status != 'resolved')
FROM installs i ORDER BY i.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Install
	for rows.Next() {
		in, err := scanInstall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// GetInstall returns one install or nil when unknown.
func (s *Store) GetInstall(id string) (*Install, error) {
	row := s.db.QueryRow(`
SELECT i.id, i.name, i.version, i.os, i.arch, i.first_seen, i.last_seen, i.last_snapshot,
	(SELECT COUNT(*) FROM tickets t WHERE t.install_id = i.id AND t.status != 'resolved')
FROM installs i WHERE i.id = ?`, id)
	in, err := scanInstall(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return in, err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanInstall(r rowScanner) (*Install, error) {
	var in Install
	var snap sql.NullString
	if err := r.Scan(&in.ID, &in.Name, &in.Version, &in.OS, &in.Arch,
		&in.FirstSeen, &in.LastSeen, &snap, &in.OpenTickets); err != nil {
		return nil, err
	}
	if snap.Valid {
		var u UsageSnapshot
		if err := json.Unmarshal([]byte(snap.String), &u); err == nil {
			in.Usage = &u
		}
	}
	return &in, nil
}

// SnapshotHistory returns up to limit recent snapshots for an install,
// newest first.
func (s *Store) SnapshotHistory(installID string, limit int) ([]*Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT at, data FROM snapshots WHERE install_id = ? ORDER BY at DESC, id DESC LIMIT ?`,
		installID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Snapshot
	for rows.Next() {
		var sn Snapshot
		var data string
		if err := rows.Scan(&sn.At, &data); err != nil {
			return nil, err
		}
		var u UsageSnapshot
		if err := json.Unmarshal([]byte(data), &u); err == nil {
			sn.Data = &u
		}
		out = append(out, &sn)
	}
	return out, rows.Err()
}
