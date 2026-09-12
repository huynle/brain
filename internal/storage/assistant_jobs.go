// Conversation jobs persist work independently of HTTP turns.
// The store is private runtime state, never indexed as Brain content.
package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

var ErrNotFound = errors.New("conversation job not found")

type Job struct {
	ID           string `json:"id"`
	Conversation string `json:"conversation_id"`
	Title        string `json:"title"`
	Project      string `json:"project"`
	TaskID       string `json:"task_id"`
	TaskPath     string `json:"task_path"`
	RunnerID     string `json:"runner_id"`
	State        string `json:"state"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	Revision     int    `json:"revision"`
	Acknowledged int    `json:"acknowledged"`
	Created      string `json:"created_at"`
	Updated      string `json:"updated_at"`
}

// Record contains server-only execution state. Never send this to a client.
// Credential is a delegated bearer token, protected by the private directory
// and SQLite file permissions. MCP revalidates it on every tool call.
type Record struct {
	Job
	Owner      string
	Credential string
	Request    json.RawMessage
	Messages   json.RawMessage
	Pending    []string
}

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS assistant_jobs(id TEXT PRIMARY KEY, owner TEXT NOT NULL, data TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS assistant_conversations(owner TEXT NOT NULL,id TEXT NOT NULL,title TEXT NOT NULL,history TEXT NOT NULL,PRIMARY KEY(owner,id))`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

type Conversation struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	History json.RawMessage `json:"history"`
}

func (s *Store) ReadConversation(owner, id string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Conversation
	var raw string
	err := s.db.QueryRow(`SELECT id,title,history FROM assistant_conversations WHERE owner=? AND id=?`, owner, id).Scan(&c.ID, &c.Title, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.History = json.RawMessage(raw)
	return c, err
}
func (s *Store) ConversationSummaries(owner string) ([]Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,title FROM assistant_conversations WHERE owner=?`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.Title); err != nil {
			return nil, err
		}
		c.History = json.RawMessage(`[]`)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Conversations(owner string) ([]Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,title,history FROM assistant_conversations WHERE owner=?`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var b string
		if err = rows.Scan(&c.ID, &c.Title, &b); err != nil {
			return nil, err
		}
		c.History = json.RawMessage(b)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CommitResponse atomically records the coordinator reply and inbox delivery.
// A disconnected phone can recover that reply from the server conversation.
func (s *Store) CommitResponse(owner string, c Conversation, delivered map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updates := []Record{}
	for id, revision := range delivered {
		r, err := s.get(owner, id)
		if err != nil {
			return err
		}
		if r.Conversation != c.ID {
			return ErrNotFound
		}
		if revision > r.Revision {
			return errors.New("invalid inbox revision")
		}
		if revision > r.Acknowledged {
			r.Acknowledged = revision
			updates = append(updates, r)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO assistant_conversations(owner,id,title,history) VALUES(?,?,?,?) ON CONFLICT(owner,id) DO UPDATE SET history=excluded.history`, owner, c.ID, c.Title, string(c.History))
	if err != nil {
		return err
	}
	for _, r := range updates {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE assistant_jobs SET data=? WHERE owner=? AND id=?`, string(b), owner, r.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(r Record) error {
	if r.Owner == "" || r.ID == "" || r.Conversation == "" {
		return errors.New("job owner, id, and conversation are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Revision = 1
	r.Created = time.Now().UTC().Format(time.RFC3339Nano)
	r.Updated = r.Created
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO assistant_jobs(id,owner,data) VALUES(?,?,?)`, r.ID, r.Owner, string(b))
	return err
}
func (s *Store) get(owner, id string) (Record, error) {
	var b string
	var r Record
	err := s.db.QueryRow(`SELECT data FROM assistant_jobs WHERE owner=? AND id=?`, owner, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(b), &r)
	return r, err
}
func (s *Store) Get(owner, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(owner, id)
}
func (s *Store) Update(owner, id string, fn func(*Record) error) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.get(owner, id)
	if err != nil {
		return r, err
	}
	if err = fn(&r); err != nil {
		return r, err
	}
	r.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	_, err = s.db.Exec(`UPDATE assistant_jobs SET data=? WHERE owner=? AND id=?`, string(b), owner, id)
	return r, err
}

// All is only for the trusted runner scheduler. HTTP callers must use List.
func (s *Store) All() ([]Record, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.list("", "") }

// NotificationJobs reads only recent terminal job summaries, never prompts,
// transcripts or delegated credentials, for the background push dispatcher.
func (s *Store) NotificationJobs() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT owner, json_extract(data,'$.id'), json_extract(data,'$.state'), json_extract(data,'$.revision'), json_extract(data,'$.updated_at') FROM assistant_jobs WHERE json_extract(data,'$.state') IN ('completed','failed','paused','cancelled') AND json_extract(data,'$.updated_at') >= ?`, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Owner, &r.ID, &r.State, &r.Revision, &r.Updated); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Store) List(owner, conversation string) ([]Record, error) {
	if owner == "" {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(owner, conversation)
}
func (s *Store) list(owner, conversation string) ([]Record, error) {
	q := `SELECT data FROM assistant_jobs`
	args := []any{}
	if owner != "" {
		q += ` WHERE owner=?`
		args = append(args, owner)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var b string
		var r Record
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			return nil, err
		}
		if conversation == "" || r.Conversation == conversation {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// Recover never replays a possibly completed tool after a process crash.
func (s *Store) Recover() error {
	all, err := s.All()
	if err != nil {
		return err
	}
	for _, r := range all {
		if r.State == "running" || r.State == "creating" {
			_, err = s.Update(r.Owner, r.ID, func(r *Record) error {
				r.State = "paused"
				r.Error = "Server restarted during execution. Review saved context before resuming; the last tool may have taken effect."
				r.Revision++
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
