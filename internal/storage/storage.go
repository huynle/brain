package storage

import (
	"database/sql"
	"errors"
	"fmt"

	// Import the pure-Go SQLite driver for side effects (driver registration).
	_ "github.com/glebarez/go-sqlite"
)

// StorageLayer wraps a *sql.DB with schema management and query methods.
type StorageLayer struct {
	db *sql.DB
}

// New opens a SQLite database at dbPath, sets PRAGMAs, and initializes the schema.
func New(dbPath string) (*StorageLayer, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return newFromDB(db)
}

// NewWithDB wraps an existing *sql.DB connection, sets PRAGMAs, and initializes the schema.
// Useful for testing with :memory: databases.
func NewWithDB(db *sql.DB) (*StorageLayer, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return newFromDB(db)
}

// newFromDB is the shared constructor that sets PRAGMAs and runs schema init.
func newFromDB(db *sql.DB) (*StorageLayer, error) {
	// Serialize writes — SQLite only supports one writer at a time.
	db.SetMaxOpenConns(1)

	// Set PRAGMAs for performance and correctness.
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	// Initialize schema (idempotent).
	if err := InitSchema(db); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &StorageLayer{db: db}, nil
}

// DB returns the underlying *sql.DB connection.
func (s *StorageLayer) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection.
func (s *StorageLayer) Close() error {
	return s.db.Close()
}
