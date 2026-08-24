package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	// Import the pure-Go SQLite driver for side effects (driver registration).
	_ "github.com/glebarez/go-sqlite"
)

// StorageLayer wraps a *sql.DB with schema management and query methods.
type StorageLayer struct {
	db *sql.DB
}

// connectionPragmas are applied by the DRIVER as each connection opens, via the
// DSN, rather than by db.Exec after the fact.
//
// This matters because foreign_keys and synchronous are PER-CONNECTION settings.
// Running them through db.Exec applies them to whichever single pooled
// connection happens to serve that call; every other connection gets SQLite's
// defaults. Measured directly against the driver with a 4-connection pool:
//
//	db.Exec form:  conn 0 -> foreign_keys=1 synchronous=1
//	               conn 1..3 -> foreign_keys=0 synchronous=2
//	DSN form:      conn 0..3 -> foreign_keys=1 synchronous=1
//
// Only journal_mode survived, because WAL is persisted in the database file.
//
// So SetMaxOpenConns(1) below was load-bearing for correctness, not just for
// write serialisation: it was the only reason foreign key enforcement was
// active at all. Raising the pool without this change would have silently
// disabled FK enforcement on nearly every connection and quietly reverted
// synchronous to FULL.
//
// busy_timeout is included because it is the setting that makes a pool larger
// than one survivable at all — without it a second writer fails immediately
// with SQLITE_BUSY instead of waiting.
var connectionPragmas = []string{
	"journal_mode(WAL)",
	"foreign_keys(1)",
	"synchronous(NORMAL)",
	"busy_timeout(5000)",
}

// dsnWithPragmas builds a driver DSN that applies connectionPragmas to every
// connection the pool opens.
func dsnWithPragmas(dbPath string) string {
	dsn := "file:" + dbPath
	sep := "?"
	for _, p := range connectionPragmas {
		dsn += sep + "_pragma=" + url.QueryEscape(p)
		sep = "&"
	}
	return dsn
}

// New opens a SQLite database at dbPath, sets PRAGMAs, and initializes the schema.
func New(dbPath string) (*StorageLayer, error) {
	db, err := sql.Open("sqlite", dsnWithPragmas(dbPath))
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
	// One connection for the whole process.
	//
	// DELIBERATELY UNCHANGED, and not merely about writes. Callers reaching
	// newFromDB through NewWithDB supply their own *sql.DB whose DSN this
	// package does not control — including the ":memory:" databases used
	// throughout the tests, where a second connection would open a SEPARATE
	// empty database rather than share this one. The cap is what makes that
	// path work at all.
	//
	// The cost is real and is now the leading candidate for a change of its
	// own: startup runs a synchronous reindex (~33s for ~70k entries) holding
	// this single connection, so every request in that window queues behind it
	// and callers that time out surface as 500s (see the Logger middleware in
	// internal/api). WAL supports concurrent readers alongside one writer, so
	// serialising reads is self-inflicted.
	//
	// What blocked raising it was that the pragmas below only ever bound to one
	// connection. That blocker is now cleared for the New path by
	// dsnWithPragmas. What remains before raising it is verifying that no
	// read-modify-write sequence in this package relies on the implicit
	// serialisation the cap provides — which is a separate change with separate
	// testing, not a rider on this one.
	db.SetMaxOpenConns(1)

	// Belt-and-braces for the NewWithDB path, whose DSN we do not control.
	// These bind only to the connection that serves them, which is sufficient
	// precisely because of the cap above. New() additionally sets them in the
	// DSN so they hold for every connection.
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
