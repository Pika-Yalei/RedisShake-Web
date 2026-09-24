package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type store struct {
	db   *sql.DB
	aead cipher.AEAD
	dir  string
}

type Connection struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Address          string `json:"address"`
	Username         string `json:"username"`
	Password         string `json:"password,omitempty"`
	SentinelMaster   string `json:"sentinelMaster,omitempty"`
	SentinelAddress  string `json:"sentinelAddress,omitempty"`
	SentinelUsername string `json:"sentinelUsername,omitempty"`
	SentinelPassword string `json:"sentinelPassword,omitempty"`
}

func (c Connection) safe() Connection {
	c.Password, c.SentinelPassword = "", ""
	return c
}

type Rules struct {
	AllowPrefixes []string `json:"allowPrefixes"`
	BlockPrefixes []string `json:"blockPrefixes"`
	AllowRegex    []string `json:"allowRegex"`
	BlockRegex    []string `json:"blockRegex"`
	AllowCommands []string `json:"allowCommands"`
	BlockCommands []string `json:"blockCommands"`
}

type Task struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	SourceID     string         `json:"sourceId"`
	TargetID     string         `json:"targetId"`
	DBMap        map[string]int `json:"dbMap"`
	Rules        Rules          `json:"rules"`
	TargetPolicy string         `json:"targetPolicy"`
	UpdatedAt    string         `json:"updatedAt"`
}

type Run struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Error     string `json:"error,omitempty"`
	PID       int    `json:"pid,omitempty"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
}

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "secret.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		err = os.WriteFile(keyPath, key, 0600)
	}
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("secret key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"CREATE TABLE IF NOT EXISTS admin (id INTEGER PRIMARY KEY CHECK(id=1), username TEXT NOT NULL, password_hash BLOB NOT NULL)",
		"CREATE TABLE IF NOT EXISTS sessions (digest TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)",
		"CREATE TABLE IF NOT EXISTS connections (id TEXT PRIMARY KEY, name TEXT NOT NULL, payload TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, name TEXT NOT NULL, payload TEXT NOT NULL, updated_at TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, status TEXT NOT NULL, phase TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', pid INTEGER NOT NULL DEFAULT 0, started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '')",
		"CREATE INDEX IF NOT EXISTS idx_runs_task ON runs(task_id, started_at)",
		"CREATE UNIQUE INDEX IF NOT EXISTS one_active_run ON runs(task_id) WHERE status IN ('STARTING','RUNNING','STOPPING')",
		"CREATE TABLE IF NOT EXISTS clear_requests (digest TEXT PRIMARY KEY, task_id TEXT NOT NULL, target_digest TEXT NOT NULL, expires_at INTEGER NOT NULL, status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '')",
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	columns, err := db.Query("PRAGMA table_info(admin)")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	hasUsername := false
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = columns.Close()
			_ = db.Close()
			return nil, err
		}
		if name == "username" {
			hasUsername = true
		}
	}
	if err := columns.Err(); err != nil {
		_ = columns.Close()
		_ = db.Close()
		return nil, err
	}
	_ = columns.Close()
	if !hasUsername {
		if _, err := db.Exec("ALTER TABLE admin ADD COLUMN username TEXT NOT NULL DEFAULT 'admin'"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	var adminID int
	err = db.QueryRow("SELECT id FROM admin WHERE id=1").Scan(&adminID)
	if errors.Is(err, sql.ErrNoRows) {
		var hash string
		hash, err = passwordHash(defaultAdminPassword)
		if err == nil {
			_, err = db.Exec("INSERT INTO admin(id,username,password_hash) VALUES(1,?,?)", defaultAdminUsername, hash)
		}
	}
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize default administrator: %w", err)
	}
	return &store{db: db, aead: aead, dir: dir}, nil
}

func (s *store) seal(value any) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, nil)), nil
}

func (s *store) open(ciphertext string, value any) error {
	bytes, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil || len(bytes) < s.aead.NonceSize() {
		return errors.New("invalid encrypted record")
	}
	plain, err := s.aead.Open(nil, bytes[:s.aead.NonceSize()], bytes[s.aead.NonceSize():], nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, value)
}

func (s *store) saveConnection(c Connection) error {
	payload, err := s.seal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO connections(id,name,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,payload=excluded.payload", c.ID, c.Name, payload)
	return err
}

func (s *store) connection(id string) (Connection, error) {
	var payload string
	err := s.db.QueryRow("SELECT payload FROM connections WHERE id=?", id).Scan(&payload)
	if err != nil {
		return Connection{}, err
	}
	var c Connection
	err = s.open(payload, &c)
	return c, err
}

func (s *store) connections() ([]Connection, error) {
	rows, err := s.db.Query("SELECT payload FROM connections ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var c Connection
		if err := s.open(payload, &c); err != nil {
			return nil, err
		}
		out = append(out, c.safe())
	}
	return out, rows.Err()
}

func (s *store) saveTask(t Task) error {
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	payload, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO tasks(id,name,payload,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,payload=excluded.payload,updated_at=excluded.updated_at", t.ID, t.Name, string(payload), t.UpdatedAt)
	return err
}

func (s *store) task(id string) (Task, error) {
	var payload string
	err := s.db.QueryRow("SELECT payload FROM tasks WHERE id=?", id).Scan(&payload)
	if err != nil {
		return Task{}, err
	}
	var t Task
	err = json.Unmarshal([]byte(payload), &t)
	return t, err
}

func (s *store) tasks() ([]Task, error) {
	rows, err := s.db.Query("SELECT payload FROM tasks ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var t Task
		if err := json.Unmarshal([]byte(payload), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *store) runs(taskID string) ([]Run, error) {
	rows, err := s.db.Query("SELECT id,task_id,status,phase,error,pid,started_at,ended_at FROM runs WHERE task_id=? ORDER BY started_at DESC", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.TaskID, &run.Status, &run.Phase, &run.Error, &run.PID, &run.StartedAt, &run.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *store) activeRun(taskID string) (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM runs WHERE task_id=? AND status IN ('STARTING','RUNNING','STOPPING')", taskID).Scan(&n)
	return n > 0, err
}

func (s *store) markInterrupted() error {
	_, err := s.db.Exec("UPDATE runs SET status='FAILED', error='执行器重启，同步已中断；请确认后重新全量运行', ended_at=? WHERE status IN ('STARTING','RUNNING','STOPPING')", time.Now().UTC().Format(time.RFC3339))
	return err
}
