package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"mybot/internal/core"
)

// txExecer is satisfied by both *sql.Tx and *sql.DB so that prepared-style
// helpers can work inside or outside a transaction.
type txExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

type SQLiteStore struct {
	writeDB *sql.DB // single writer connection
	readDB  *sql.DB // multiple reader connections (WAL)
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS threads (
    thread_id        INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL DEFAULT '',
    updated_at_ms    INTEGER NOT NULL DEFAULT 0,
    last_activity_ms INTEGER NOT NULL DEFAULT 0,
    deleted          INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS users (
    user_id       INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL DEFAULT '',
    updated_at_ms INTEGER NOT NULL DEFAULT 0,
    deleted       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    message_id           TEXT PRIMARY KEY,
    thread_id            INTEGER NOT NULL DEFAULT 0,
    sender_id            INTEGER NOT NULL DEFAULT 0,
    sender_name_snapshot TEXT    NOT NULL DEFAULT '',
    text                 TEXT    NOT NULL DEFAULT '',
    reply_to_message_id  TEXT    NOT NULL DEFAULT '',
    offline_threading_id TEXT    NOT NULL DEFAULT '',
    is_from_bot          INTEGER NOT NULL DEFAULT 0,
    has_media            INTEGER NOT NULL DEFAULT 0,
    attachments_json     TEXT    NOT NULL DEFAULT '[]',
    timestamp_ms         INTEGER NOT NULL DEFAULT 0,
    edit_count           INTEGER NOT NULL DEFAULT 0,
    is_edited            INTEGER NOT NULL DEFAULT 0,
    is_recalled          INTEGER NOT NULL DEFAULT 0,
    created_at_ms        INTEGER NOT NULL DEFAULT 0,
    updated_at_ms        INTEGER NOT NULL DEFAULT 0,
    recalled_at_ms       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_messages_thread_ts
    ON messages(thread_id, timestamp_ms, message_id);

CREATE TABLE IF NOT EXISTS thread_last_bot (
    thread_id  INTEGER PRIMARY KEY,
    message_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// OpenSQLiteStore opens a SQLite store with separate read and write connections.
// readPoolSize controls the number of reader connections (WAL mode allows concurrent readers).
func OpenSQLiteStore(path string, readPoolSize ...int) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("empty sqlite db path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	poolSize := 4
	if len(readPoolSize) > 0 && readPoolSize[0] > 0 {
		poolSize = readPoolSize[0]
	}

	// ── Write connection ────────────────────────────────────────────────
	writeDSN := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL", path)
	writeDB, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	writeDB.SetMaxOpenConns(1) // single writer

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := writeDB.ExecContext(ctx, sqliteSchema); err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if _, err := writeDB.ExecContext(ctx, `INSERT OR REPLACE INTO meta(key, value) VALUES('schema_version','3')`); err != nil {
		_ = writeDB.Close()
		return nil, err
	}

	// Optimize WAL settings on the write connection.
	for _, pragma := range []string{
		"PRAGMA wal_autocheckpoint=1000",
		"PRAGMA cache_size=-8000",    // 8 MB
		"PRAGMA mmap_size=268435456", // 256 MB
		"PRAGMA temp_store=MEMORY",
	} {
		if _, err := writeDB.ExecContext(ctx, pragma); err != nil {
			_ = writeDB.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	// ── Read connection pool ────────────────────────────────────────────
	readDSN := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&mode=ro", path)
	readDB, err := sql.Open("sqlite", readDSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	readDB.SetMaxOpenConns(poolSize)
	readDB.SetMaxIdleConns(poolSize)

	// Warm the read pool.
	if err := readDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("ping read db: %w", err)
	}

	return &SQLiteStore{writeDB: writeDB, readDB: readDB}, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	if s.readDB != nil {
		if err := s.readDB.Close(); err != nil {
			firstErr = err
		}
	}
	if s.writeDB != nil {
		if err := s.writeDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ExecBatch runs fn inside a single write transaction.  Used by WriteBatcher.
func (s *SQLiteStore) ExecBatch(fn func(tx txExecer) error) error {
	tx, err := s.writeDB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ── Threads ─────────────────────────────────────────────────────────────────

const upsertThreadSQL = `
	INSERT INTO threads(thread_id, name, updated_at_ms, last_activity_ms, deleted)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		name             = CASE WHEN excluded.name != '' THEN excluded.name ELSE threads.name END,
		updated_at_ms    = excluded.updated_at_ms,
		last_activity_ms = CASE WHEN excluded.last_activity_ms > 0 THEN excluded.last_activity_ms ELSE threads.last_activity_ms END,
		deleted          = excluded.deleted`

func (s *SQLiteStore) UpsertThread(_ context.Context, rec *core.ThreadRecord) error {
	if rec == nil || rec.ThreadID == 0 {
		return nil
	}
	_, err := s.writeDB.Exec(upsertThreadSQL,
		rec.ThreadID, rec.Name, rec.UpdatedAtUnixMs, rec.LastActivityMs, boolToInt(rec.Deleted))
	return err
}

func (s *SQLiteStore) upsertThreadTx(tx txExecer, rec *core.ThreadRecord) error {
	if rec == nil || rec.ThreadID == 0 {
		return nil
	}
	_, err := tx.Exec(upsertThreadSQL,
		rec.ThreadID, rec.Name, rec.UpdatedAtUnixMs, rec.LastActivityMs, boolToInt(rec.Deleted))
	return err
}

func (s *SQLiteStore) GetThread(_ context.Context, threadID int64) (*core.ThreadRecord, error) {
	row := s.readDB.QueryRow(`SELECT thread_id, name, updated_at_ms, last_activity_ms, deleted FROM threads WHERE thread_id = ?`, threadID)
	rec := &core.ThreadRecord{}
	var deleted int
	err := row.Scan(&rec.ThreadID, &rec.Name, &rec.UpdatedAtUnixMs, &rec.LastActivityMs, &deleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Deleted = deleted != 0
	return rec, nil
}

// ── Users ───────────────────────────────────────────────────────────────────

const upsertUserSQL = `
	INSERT INTO users(user_id, name, updated_at_ms, deleted)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(user_id) DO UPDATE SET
		name          = CASE WHEN excluded.name != '' THEN excluded.name ELSE users.name END,
		updated_at_ms = excluded.updated_at_ms,
		deleted       = excluded.deleted`

func (s *SQLiteStore) UpsertUser(_ context.Context, rec *core.UserRecord) error {
	if rec == nil || rec.UserID == 0 {
		return nil
	}
	_, err := s.writeDB.Exec(upsertUserSQL,
		rec.UserID, rec.Name, rec.UpdatedAtUnixMs, boolToInt(rec.Deleted))
	return err
}

func (s *SQLiteStore) upsertUserTx(tx txExecer, rec *core.UserRecord) error {
	if rec == nil || rec.UserID == 0 {
		return nil
	}
	_, err := tx.Exec(upsertUserSQL,
		rec.UserID, rec.Name, rec.UpdatedAtUnixMs, boolToInt(rec.Deleted))
	return err
}

func (s *SQLiteStore) GetUser(_ context.Context, userID int64) (*core.UserRecord, error) {
	row := s.readDB.QueryRow(`SELECT user_id, name, updated_at_ms, deleted FROM users WHERE user_id = ?`, userID)
	rec := &core.UserRecord{}
	var deleted int
	err := row.Scan(&rec.UserID, &rec.Name, &rec.UpdatedAtUnixMs, &deleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Deleted = deleted != 0
	return rec, nil
}

// ── Messages ────────────────────────────────────────────────────────────────

const upsertMessageSQL = `
	INSERT INTO messages(
		message_id, thread_id, sender_id, sender_name_snapshot, text,
		reply_to_message_id, offline_threading_id, is_from_bot, has_media,
		attachments_json, timestamp_ms, edit_count, is_edited, is_recalled,
		created_at_ms, updated_at_ms, recalled_at_ms
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(message_id) DO UPDATE SET
		thread_id            = excluded.thread_id,
		sender_id            = excluded.sender_id,
		sender_name_snapshot = excluded.sender_name_snapshot,
		text                 = excluded.text,
		reply_to_message_id  = excluded.reply_to_message_id,
		offline_threading_id = excluded.offline_threading_id,
		is_from_bot          = excluded.is_from_bot,
		has_media            = excluded.has_media,
		attachments_json     = excluded.attachments_json,
		timestamp_ms         = excluded.timestamp_ms,
		edit_count           = excluded.edit_count,
		is_edited            = excluded.is_edited,
		is_recalled          = excluded.is_recalled,
		created_at_ms        = CASE WHEN messages.created_at_ms > 0 THEN messages.created_at_ms ELSE excluded.created_at_ms END,
		updated_at_ms        = excluded.updated_at_ms,
		recalled_at_ms       = excluded.recalled_at_ms`

func messageArgs(rec *core.MessageRecord) []any {
	attachJSON, err := json.Marshal(rec.Attachments)
	if err != nil {
		attachJSON = []byte("[]")
	}
	return []any{
		rec.MessageID, rec.ThreadID, rec.SenderID, rec.SenderNameSnapshot, rec.Text,
		rec.ReplyToMessageID, rec.OfflineThreadingID, boolToInt(rec.IsFromBot), boolToInt(rec.HasMedia),
		string(attachJSON), rec.TimestampMs, rec.EditCount, boolToInt(rec.IsEdited), boolToInt(rec.IsRecalled),
		rec.CreatedAtUnixMs, rec.UpdatedAtUnixMs, rec.RecalledAtUnixMs,
	}
}

func (s *SQLiteStore) UpsertMessage(_ context.Context, rec *core.MessageRecord) error {
	if rec == nil || rec.MessageID == "" {
		return nil
	}
	_, err := s.writeDB.Exec(upsertMessageSQL, messageArgs(rec)...)
	return err
}

func (s *SQLiteStore) upsertMessageTx(tx txExecer, rec *core.MessageRecord) error {
	if rec == nil || rec.MessageID == "" {
		return nil
	}
	_, err := tx.Exec(upsertMessageSQL, messageArgs(rec)...)
	return err
}

func (s *SQLiteStore) GetMessage(_ context.Context, messageID string) (*core.MessageRecord, error) {
	if messageID == "" {
		return nil, nil
	}
	return s.scanMessage(s.readDB.QueryRow(`
		SELECT message_id, thread_id, sender_id, sender_name_snapshot, text,
		       reply_to_message_id, offline_threading_id, is_from_bot, has_media,
		       attachments_json, timestamp_ms, edit_count, is_edited, is_recalled,
		       created_at_ms, updated_at_ms, recalled_at_ms
		FROM messages WHERE message_id = ?`, messageID))
}

func (s *SQLiteStore) ListThreadMessages(_ context.Context, threadID int64, limit int, beforeMessageID string) ([]*core.MessageRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if beforeMessageID != "" {
		rows, err = s.readDB.Query(`
			SELECT m.message_id, m.thread_id, m.sender_id, m.sender_name_snapshot, m.text,
			       m.reply_to_message_id, m.offline_threading_id, m.is_from_bot, m.has_media,
			       m.attachments_json, m.timestamp_ms, m.edit_count, m.is_edited, m.is_recalled,
			       m.created_at_ms, m.updated_at_ms, m.recalled_at_ms
			FROM messages m
			WHERE m.thread_id = ?
			  AND (m.timestamp_ms, m.message_id) < (
			      SELECT timestamp_ms, message_id FROM messages WHERE message_id = ?
			  )
			ORDER BY m.timestamp_ms DESC, m.message_id DESC
			LIMIT ?
		`, threadID, beforeMessageID, limit)
	} else {
		rows, err = s.readDB.Query(`
			SELECT message_id, thread_id, sender_id, sender_name_snapshot, text,
			       reply_to_message_id, offline_threading_id, is_from_bot, has_media,
			       attachments_json, timestamp_ms, edit_count, is_edited, is_recalled,
			       created_at_ms, updated_at_ms, recalled_at_ms
			FROM messages
			WHERE thread_id = ?
			ORDER BY timestamp_ms DESC, message_id DESC
			LIMIT ?
		`, threadID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*core.MessageRecord
	for rows.Next() {
		rec, err := s.scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}

// ── Last bot message ────────────────────────────────────────────────────────

const setLastBotSQL = `
	INSERT INTO thread_last_bot(thread_id, message_id) VALUES(?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET message_id = excluded.message_id`

func (s *SQLiteStore) SetLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	if threadID == 0 || messageID == "" {
		return nil
	}
	_, err := s.writeDB.Exec(setLastBotSQL, threadID, messageID)
	return err
}

func (s *SQLiteStore) setLastBotMessageTx(tx txExecer, threadID int64, messageID string) error {
	if threadID == 0 || messageID == "" {
		return nil
	}
	_, err := tx.Exec(setLastBotSQL, threadID, messageID)
	return err
}

func (s *SQLiteStore) GetLastBotMessage(ctx context.Context, threadID int64) (*core.MessageRecord, error) {
	var messageID string
	err := s.readDB.QueryRow(`SELECT message_id FROM thread_last_bot WHERE thread_id = ?`, threadID).Scan(&messageID)
	if err == sql.ErrNoRows || messageID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetMessage(ctx, messageID)
}

func (s *SQLiteStore) ClearLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	if threadID == 0 {
		return nil
	}
	if messageID != "" {
		_, err := s.writeDB.Exec(`DELETE FROM thread_last_bot WHERE thread_id = ? AND message_id = ?`, threadID, messageID)
		return err
	}
	_, err := s.writeDB.Exec(`DELETE FROM thread_last_bot WHERE thread_id = ?`, threadID)
	return err
}

func (s *SQLiteStore) clearLastBotMessageTx(tx txExecer, threadID int64, messageID string) error {
	if threadID == 0 || messageID == "" {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM thread_last_bot WHERE thread_id = ? AND message_id = ?`, threadID, messageID)
	return err
}

func (s *SQLiteStore) clearLastBotMessageByThreadTx(tx txExecer, threadID int64) error {
	if threadID == 0 {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM thread_last_bot WHERE thread_id = ?`, threadID)
	return err
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func (s *SQLiteStore) scanMessage(row *sql.Row) (*core.MessageRecord, error) {
	rec := &core.MessageRecord{}
	var isFromBot, hasMedia, isEdited, isRecalled int
	var attachJSON string
	err := row.Scan(
		&rec.MessageID, &rec.ThreadID, &rec.SenderID, &rec.SenderNameSnapshot, &rec.Text,
		&rec.ReplyToMessageID, &rec.OfflineThreadingID, &isFromBot, &hasMedia,
		&attachJSON, &rec.TimestampMs, &rec.EditCount, &isEdited, &isRecalled,
		&rec.CreatedAtUnixMs, &rec.UpdatedAtUnixMs, &rec.RecalledAtUnixMs,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.IsFromBot = isFromBot != 0
	rec.HasMedia = hasMedia != 0
	rec.IsEdited = isEdited != 0
	rec.IsRecalled = isRecalled != 0
	_ = json.Unmarshal([]byte(attachJSON), &rec.Attachments)
	return rec, nil
}

func (s *SQLiteStore) scanMessageRow(rows *sql.Rows) (*core.MessageRecord, error) {
	rec := &core.MessageRecord{}
	var isFromBot, hasMedia, isEdited, isRecalled int
	var attachJSON string
	err := rows.Scan(
		&rec.MessageID, &rec.ThreadID, &rec.SenderID, &rec.SenderNameSnapshot, &rec.Text,
		&rec.ReplyToMessageID, &rec.OfflineThreadingID, &isFromBot, &hasMedia,
		&attachJSON, &rec.TimestampMs, &rec.EditCount, &isEdited, &isRecalled,
		&rec.CreatedAtUnixMs, &rec.UpdatedAtUnixMs, &rec.RecalledAtUnixMs,
	)
	if err != nil {
		return nil, err
	}
	rec.IsFromBot = isFromBot != 0
	rec.HasMedia = hasMedia != 0
	rec.IsEdited = isEdited != 0
	rec.IsRecalled = isRecalled != 0
	_ = json.Unmarshal([]byte(attachJSON), &rec.Attachments)
	return rec, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
