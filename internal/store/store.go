// Package store keeps T3Go's projects, threads and transcript in SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store is a connection to the environment's SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create directory: %w", err)
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the database's file path.
func (s *Store) Path() string { return s.path }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS threads (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  permission TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  worktree_path TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'idle',
  provider_session TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS threads_project ON threads(project_id, archived, updated_at DESC);
CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  permission TEXT NOT NULL,
  prompt TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  ended_at TEXT,
  error TEXT NOT NULL DEFAULT '',
  checkpoint TEXT NOT NULL DEFAULT '',
  usage TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS runs_thread ON runs(thread_id, started_at);
CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  kind TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  attachments TEXT NOT NULL DEFAULT '',
  seq INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_thread ON messages(thread_id, seq);
CREATE TABLE IF NOT EXISTS uploads (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  mime TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  thread_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_thread ON events(thread_id, seq);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Databases from before attachments have the column added here.
	if err := s.ensureColumn(ctx, "messages", "attachments", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds a column to a table that does not have it yet.
func (s *Store) ensureColumn(ctx context.Context, table, column, decl string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("store: %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &def, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+decl); err != nil {
		return fmt.Errorf("store: add %s.%s: %w", table, column, err)
	}
	return nil
}

// Ping checks the database.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateProject inserts a project.
func (s *Store) CreateProject(ctx context.Context, p proto.Project) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,path,created_at,updated_at) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Path, p.CreatedAt.UTC().Format(time.RFC3339Nano), p.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: create project: %w", err)
	}
	return nil
}

// Projects lists projects by name.
func (s *Store) Projects(ctx context.Context) ([]proto.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,path,created_at,updated_at FROM projects ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("store: projects: %w", err)
	}
	defer rows.Close()
	var out []proto.Project
	for rows.Next() {
		var p proto.Project
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &created, &updated); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Project returns a project by id.
func (s *Store) Project(ctx context.Context, id string) (proto.Project, error) {
	var p proto.Project
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,path,created_at,updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Path, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, nil
}

// UpdateProject changes a project's name and path.
func (s *Store) UpdateProject(ctx context.Context, p proto.Project) error {
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, path=?, updated_at=? WHERE id=?`,
		p.Name, p.Path, p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.ID)
	if err != nil {
		return fmt.Errorf("store: update project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project and its threads.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func threadColumns() string {
	return `id,project_id,title,provider,model,permission,branch,worktree_path,status,provider_session,summary,archived,created_at,updated_at,last_error`
}

type scanner interface{ Scan(...any) error }

func scanThread(row scanner) (proto.Thread, error) {
	var t proto.Thread
	var created, updated string
	var archived int
	err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Provider, &t.Model, &t.Permission, &t.Branch, &t.WorktreePath,
		&t.Status, &t.ProviderSession, &t.Summary, &archived, &created, &updated, &t.LastError)
	if err != nil {
		return t, err
	}
	t.Archived = archived != 0
	t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
	return t, nil
}

// CreateThread inserts a thread.
func (s *Store) CreateThread(ctx context.Context, t proto.Thread) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO threads(`+threadColumns()+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ProjectID, t.Title, t.Provider, t.Model, t.Permission, t.Branch, t.WorktreePath, t.Status, t.ProviderSession,
		t.Summary, boolInt(t.Archived), t.CreatedAt.UTC().Format(time.RFC3339Nano), t.UpdatedAt.UTC().Format(time.RFC3339Nano), t.LastError)
	if err != nil {
		return fmt.Errorf("store: create thread: %w", err)
	}
	return nil
}

// Threads lists threads, newest first. projectID "" lists every project.
func (s *Store) Threads(ctx context.Context, projectID string, includeArchived bool) ([]proto.Thread, error) {
	q := `SELECT ` + threadColumns() + ` FROM threads WHERE archived=?`
	args := []any{0}
	if includeArchived {
		q = `SELECT ` + threadColumns() + ` FROM threads WHERE archived IN (0,1)`
		args = nil
	}
	if projectID != "" {
		q += ` AND project_id=?`
		args = append(args, projectID)
	}
	q += ` ORDER BY updated_at DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: threads: %w", err)
	}
	defer rows.Close()
	var out []proto.Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Thread returns a thread by id.
func (s *Store) Thread(ctx context.Context, id string) (proto.Thread, error) {
	t, err := scanThread(s.db.QueryRowContext(ctx, `SELECT `+threadColumns()+` FROM threads WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// UpdateThread writes a thread's mutable fields.
func (s *Store) UpdateThread(ctx context.Context, t proto.Thread) error {
	res, err := s.db.ExecContext(ctx, `UPDATE threads SET title=?,provider=?,model=?,permission=?,branch=?,worktree_path=?,status=?,
		provider_session=?,summary=?,archived=?,updated_at=?,last_error=? WHERE id=?`,
		t.Title, t.Provider, t.Model, t.Permission, t.Branch, t.WorktreePath, t.Status, t.ProviderSession, t.Summary,
		boolInt(t.Archived), t.UpdatedAt.UTC().Format(time.RFC3339Nano), t.LastError, t.ID)
	if err != nil {
		return fmt.Errorf("store: update thread: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteThread removes a thread and its history.
func (s *Store) DeleteThread(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM threads WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// InterruptRuns marks the runs and threads a crash left running as
// failed, so a restart does not leave a thread that can never finish.
func (s *Store) InterruptRuns(ctx context.Context, reason string, now time.Time) (runs, threads int64, err error) {
	ended := now.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, ended_at=?, error=? WHERE status=?`,
		proto.RunFailed, ended, reason, proto.RunRunning)
	if err != nil {
		return 0, 0, fmt.Errorf("store: interrupt runs: %w", err)
	}
	runs, _ = res.RowsAffected()
	res, err = s.db.ExecContext(ctx, `UPDATE threads SET status=?, last_error=?, updated_at=? WHERE status=?`,
		proto.ThreadFailed, reason, ended, proto.ThreadRunning)
	if err != nil {
		return runs, 0, fmt.Errorf("store: interrupt threads: %w", err)
	}
	threads, _ = res.RowsAffected()
	return runs, threads, nil
}

// CreateRun inserts a run.
func (s *Store) CreateRun(ctx context.Context, r proto.Run) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs(id,thread_id,status,provider,model,permission,prompt,started_at,ended_at,error,checkpoint,usage)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.ThreadID, r.Status, r.Provider, r.Model, r.Permission, r.Prompt,
		r.StartedAt.UTC().Format(time.RFC3339Nano), nullableTime(r.EndedAt), r.Error, r.Checkpoint, usageJSON(r.Usage))
	if err != nil {
		return fmt.Errorf("store: create run: %w", err)
	}
	return nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func usageJSON(u *proto.Usage) string {
	if u == nil {
		return ""
	}
	b, _ := json.Marshal(u)
	return string(b)
}

// UpdateRun writes a run's terminal state.
func (s *Store) UpdateRun(ctx context.Context, r proto.Run) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?,ended_at=?,error=?,checkpoint=?,usage=? WHERE id=?`,
		r.Status, nullableTime(r.EndedAt), r.Error, r.Checkpoint, usageJSON(r.Usage), r.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Runs lists a thread's runs oldest first.
func (s *Store) Runs(ctx context.Context, threadID string) ([]proto.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,thread_id,status,provider,model,permission,prompt,started_at,ended_at,error,checkpoint,usage
		FROM runs WHERE thread_id=? ORDER BY started_at, id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []proto.Run
	for rows.Next() {
		var r proto.Run
		var started string
		var ended sql.NullString
		var usage string
		if err := rows.Scan(&r.ID, &r.ThreadID, &r.Status, &r.Provider, &r.Model, &r.Permission, &r.Prompt, &started, &ended,
			&r.Error, &r.Checkpoint, &usage); err != nil {
			return nil, err
		}
		r.StartedAt = parseTime(started)
		if ended.Valid && ended.String != "" {
			t := parseTime(ended.String)
			r.EndedAt = &t
		}
		if usage != "" {
			var u proto.Usage
			if json.Unmarshal([]byte(usage), &u) == nil {
				r.Usage = &u
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateUpload records an uploaded file.
func (s *Store) CreateUpload(ctx context.Context, a proto.Attachment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO uploads(id,name,mime,size,created_at) VALUES(?,?,?,?,?)`,
		a.ID, a.Name, a.MimeType, a.Size, now())
	if err != nil {
		return fmt.Errorf("store: create upload: %w", err)
	}
	return nil
}

// Upload returns an uploaded file's metadata.
func (s *Store) Upload(ctx context.Context, id string) (proto.Attachment, error) {
	var a proto.Attachment
	err := s.db.QueryRowContext(ctx, `SELECT id,name,mime,size FROM uploads WHERE id=?`, id).
		Scan(&a.ID, &a.Name, &a.MimeType, &a.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// DeleteUpload forgets an uploaded file's metadata.
func (s *Store) DeleteUpload(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, id)
	return err
}

// AppendMessage inserts a message and returns its sequence.
func (s *Store) AppendMessage(ctx context.Context, m proto.Message) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE thread_id=?`, m.ThreadID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	rawAttachments, _ := json.Marshal(m.Attachments)
	_, err = s.db.ExecContext(ctx, `INSERT INTO messages(id,thread_id,run_id,role,kind,content,title,detail,status,attachments,seq,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.ThreadID, m.RunID, m.Role, m.Kind, m.Content, m.Title, m.Detail, m.Status,
		string(rawAttachments), seq, m.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("store: append message: %w", err)
	}
	return seq, nil
}

// UpdateMessage writes a message's growing or final content.
func (s *Store) UpdateMessage(ctx context.Context, m proto.Message) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET content=?,title=?,detail=?,status=? WHERE id=?`,
		m.Content, m.Title, m.Detail, m.Status, m.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Messages lists a thread's transcript oldest first, at most limit entries
// when limit is positive.
func (s *Store) Messages(ctx context.Context, threadID string, limit int) ([]proto.Message, error) {
	q := `SELECT id,thread_id,run_id,role,kind,content,title,detail,status,attachments,created_at FROM messages WHERE thread_id=? ORDER BY seq`
	args := []any{threadID}
	if limit > 0 {
		// Keep the newest limit, then restore chronological order.
		q = `SELECT id,thread_id,run_id,role,kind,content,title,detail,status,attachments,created_at FROM (` +
			`SELECT id,thread_id,run_id,role,kind,content,title,detail,status,attachments,created_at,seq FROM messages WHERE thread_id=? ORDER BY seq DESC LIMIT ?` +
			`) ORDER BY seq`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []proto.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMessage(row scanner) (proto.Message, error) {
	var m proto.Message
	var created, attachments string
	err := row.Scan(&m.ID, &m.ThreadID, &m.RunID, &m.Role, &m.Kind, &m.Content, &m.Title, &m.Detail, &m.Status, &attachments, &created)
	if err != nil {
		return m, err
	}
	m.CreatedAt = parseTime(created)
	if attachments != "" {
		if err := json.Unmarshal([]byte(attachments), &m.Attachments); err != nil {
			return m, fmt.Errorf("store: read message attachments: %w", err)
		}
	}
	return m, nil
}

// Message returns one message.
func (s *Store) Message(ctx context.Context, id string) (proto.Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT id,thread_id,run_id,role,kind,content,title,detail,status,attachments,created_at FROM messages WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Setting reads a settings value.
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting writes a settings value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// AppendEvent records an event for a short replay window.
func (s *Store) AppendEvent(ctx context.Context, e proto.Event, data string) (int64, error) {
	created := e.At
	if created.IsZero() {
		created = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO events(type,thread_id,project_id,data,created_at) VALUES(?,?,?,?,?)`,
		e.Type, e.ThreadID, e.ProjectID, data, created.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Events lists events after seq, oldest first.
func (s *Store) Events(ctx context.Context, after int64, limit int) ([]proto.Event, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq,type,thread_id,project_id,data,created_at FROM events WHERE seq>? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []proto.Event
	for rows.Next() {
		var e proto.Event
		var data, created string
		if err := rows.Scan(&e.Seq, &e.Type, &e.ThreadID, &e.ProjectID, &data, &created); err != nil {
			return nil, err
		}
		e.At = parseTime(created)
		if data != "" {
			var raw json.RawMessage = []byte(data)
			e.Data = raw
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneEvents keeps the newest retain events.
func (s *Store) PruneEvents(ctx context.Context, retain int) error {
	if retain <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE seq <= (SELECT COALESCE(MAX(seq),0)-? FROM events)`, retain)
	return err
}

// LastEventSeq is the newest event's sequence, 0 when none.
func (s *Store) LastEventSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&seq)
	return seq, err
}

// CleanEmpty removes threads with no messages.
func (s *Store) CleanEmpty(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM threads WHERE id NOT IN (SELECT DISTINCT thread_id FROM messages) AND updated_at < ?`,
		time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339Nano))
	return err
}

// NormalizePath cleans a path for storage.
func NormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// SearchMessages finds text messages whose content contains query, newest
// first, at most limit.
func (s *Store) SearchMessages(ctx context.Context, query string, limit int) ([]proto.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,thread_id,run_id,role,kind,content,title,detail,status,attachments,created_at FROM messages
		WHERE kind=? AND content LIKE ? ORDER BY created_at DESC LIMIT ?`,
		proto.KindText, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("store: search messages: %w", err)
	}
	defer rows.Close()
	var out []proto.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
