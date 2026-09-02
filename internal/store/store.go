// Package store provides a dependency-free persistence layer for the bot.
//
// The original Python project uses SQLite (SQLAlchemy) for credentials and
// mirror tasks. To keep this Go rewrite on the standard library only, this
// package implements an equivalent record store backed by a JSON file with
// atomic writes and an in-process mutex. It is safe for concurrent use within
// a single process.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// CredentialRecord mirrors the Python gDriveDB credential record: it stores
// the auth mode and the OAuth payload (refresh token / client secret) for a
// Telegram user.
type CredentialRecord struct {
	UserID  int64             `json:"user_id"`
	Mode    string            `json:"mode"` // "oauth" or "service_account"
	Payload map[string]string `json:"payload,omitempty"`
	IsSA    bool              `json:"is_service_account"`
}

// TaskRecord mirrors the Python MirrorTask row.
type TaskRecord struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	ChatID   int64  `json:"chat_id"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Stage    string `json:"stage"`
}

// document is the on-disk schema.
type document struct {
	Credentials map[int64]CredentialRecord `json:"credentials"`
	Tasks       map[int64]TaskRecord       `json:"tasks"`
}

// Store is a file-backed record store.
type Store struct {
	path string
	mu   sync.Mutex
}

// New opens (or creates) a store at path.
func New(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

func (s *Store) load() document {
	var doc document
	if doc.Credentials == nil {
		doc.Credentials = map[int64]CredentialRecord{}
	}
	if doc.Tasks == nil {
		doc.Tasks = map[int64]TaskRecord{}
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return doc
	}
	_ = json.Unmarshal(b, &doc)
	if doc.Credentials == nil {
		doc.Credentials = map[int64]CredentialRecord{}
	}
	if doc.Tasks == nil {
		doc.Tasks = map[int64]TaskRecord{}
	}
	return doc
}

// save atomically writes the document.
func (s *Store) save(doc document) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// SaveCredential upserts a credential record.
func (s *Store) SaveCredential(rec CredentialRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	doc.Credentials[rec.UserID] = rec
	return s.save(doc)
}

// GetCredential returns the credential record for a user, or nil.
func (s *Store) GetCredential(userID int64) *CredentialRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	rec, ok := doc.Credentials[userID]
	if !ok {
		return nil
	}
	return &rec
}

// DeleteCredential removes a user's credential record (revoke).
func (s *Store) DeleteCredential(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	delete(doc.Credentials, userID)
	return s.save(doc)
}

// SaveTask upserts a task record.
func (s *Store) SaveTask(rec TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	doc.Tasks[rec.ID] = rec
	return s.save(doc)
}

// GetTask returns a task record by ID, or nil.
func (s *Store) GetTask(id int64) *TaskRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	rec, ok := doc.Tasks[id]
	if !ok {
		return nil
	}
	return &rec
}
