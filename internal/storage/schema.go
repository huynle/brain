package storage

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the latest schema version.
const CurrentSchemaVersion = 5

// ---------------------------------------------------------------------------
// DDL statements
// ---------------------------------------------------------------------------

const createNotesTable = `
CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT UNIQUE NOT NULL,
  short_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  lead TEXT DEFAULT '',
  body TEXT DEFAULT '',
  raw_content TEXT DEFAULT '',
  word_count INTEGER DEFAULT 0,
  checksum TEXT,
  metadata TEXT DEFAULT '{}',
  type TEXT,
  status TEXT,
  priority TEXT,
  project_id TEXT,
  feature_id TEXT,
  created TEXT,
  modified TEXT,
  indexed_at TEXT DEFAULT (datetime('now'))
);`

const createLinksTable = `
CREATE TABLE IF NOT EXISTS links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_path TEXT NOT NULL,
  target_id INTEGER REFERENCES notes(id) ON DELETE SET NULL,
  title TEXT DEFAULT '',
  href TEXT NOT NULL,
  type TEXT DEFAULT 'markdown',
  snippet TEXT DEFAULT ''
);`

const createTagsTable = `
CREATE TABLE IF NOT EXISTS tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag TEXT NOT NULL
);`

const createEntryMetaTable = `
CREATE TABLE IF NOT EXISTS entry_meta (
  path TEXT PRIMARY KEY,
  project_id TEXT,
  access_count INTEGER DEFAULT 0,
  last_accessed TEXT,
  last_verified TEXT,
  created_at TEXT DEFAULT (datetime('now'))
);`

const createGeneratedTasksTable = `
CREATE TABLE IF NOT EXISTS generated_tasks (
  key TEXT PRIMARY KEY,
  task_path TEXT NOT NULL,
  feature_id TEXT,
  created_at TEXT DEFAULT (datetime('now'))
);`

const createSchemaVersionTable = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
);`

const createAPITokensTable = `
CREATE TABLE IF NOT EXISTS api_tokens (
  name TEXT PRIMARY KEY,
  token TEXT UNIQUE NOT NULL,
  created_at TEXT DEFAULT (datetime('now')),
  last_used TEXT,
  revoked_at TEXT
);`

const createOAuthClientsTable = `
CREATE TABLE IF NOT EXISTS oauth_clients (
  client_id TEXT PRIMARY KEY,
  client_secret TEXT NOT NULL,
  redirect_uris TEXT NOT NULL,
  client_name TEXT,
  client_uri TEXT,
  logo_uri TEXT,
  scope TEXT,
  grant_types TEXT NOT NULL,
  response_types TEXT NOT NULL,
  token_endpoint_auth_method TEXT NOT NULL DEFAULT 'client_secret_post',
  created_at INTEGER NOT NULL
);`

const createOAuthAuthCodesTable = `
CREATE TABLE IF NOT EXISTS oauth_auth_codes (
  code TEXT PRIMARY KEY,
  client_id TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  scope TEXT,
  code_challenge TEXT NOT NULL,
  code_challenge_method TEXT NOT NULL DEFAULT 'S256',
  user_id TEXT,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id)
);`

const createOAuthAccessTokensTable = `
CREATE TABLE IF NOT EXISTS oauth_access_tokens (
  token TEXT PRIMARY KEY,
  client_id TEXT NOT NULL,
  scope TEXT,
  user_id TEXT,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);`

const createOAuthRefreshTokensTable = `
CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
  token TEXT PRIMARY KEY,
  client_id TEXT NOT NULL,
  scope TEXT,
  user_id TEXT,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);`

const createWebhooksTable = `
CREATE TABLE IF NOT EXISTS webhooks (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  events TEXT NOT NULL,
  filter TEXT DEFAULT '{}',
  secret TEXT DEFAULT '',
  enabled INTEGER DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const createWebhookDeliveriesTable = `
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id TEXT PRIMARY KEY,
  webhook_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  status_code INTEGER,
  success INTEGER NOT NULL,
  latency_ms INTEGER,
  error TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
);`

// ---------------------------------------------------------------------------
// Indexes
// ---------------------------------------------------------------------------

var createIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_notes_short_id ON notes(short_id);",
	"CREATE INDEX IF NOT EXISTS idx_notes_type ON notes(type);",
	"CREATE INDEX IF NOT EXISTS idx_notes_status ON notes(status);",
	"CREATE INDEX IF NOT EXISTS idx_notes_project ON notes(project_id);",
	"CREATE INDEX IF NOT EXISTS idx_notes_feature ON notes(feature_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_source ON links(source_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_target_path ON links(target_path);",
	"CREATE INDEX IF NOT EXISTS idx_tags_note ON tags(note_id);",
	"CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);",
	// OAuth indexes
	"CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_client ON oauth_auth_codes(client_id);",
	"CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_expires ON oauth_auth_codes(expires_at);",
	"CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_client ON oauth_access_tokens(client_id);",
	"CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at);",
	"CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_client ON oauth_refresh_tokens(client_id);",
	"CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_expires ON oauth_refresh_tokens(expires_at);",
	// Webhook indexes
	"CREATE INDEX IF NOT EXISTS idx_webhooks_enabled ON webhooks(enabled);",
	"CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id);",
	"CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at);",
}

// ---------------------------------------------------------------------------
// FTS5
// ---------------------------------------------------------------------------

const createFTS5Table = `
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
  title, body, path,
  content=notes, content_rowid=id,
  tokenize='porter unicode61'
);`

// ---------------------------------------------------------------------------
// FTS5 sync triggers
// ---------------------------------------------------------------------------

const createTriggerAfterInsert = `
CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body, path) VALUES (new.id, new.title, new.body, new.path);
END;`

const createTriggerAfterDelete = `
CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, path) VALUES('delete', old.id, old.title, old.body, old.path);
END;`

const createTriggerAfterUpdate = `
CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, path) VALUES('delete', old.id, old.title, old.body, old.path);
  INSERT INTO notes_fts(rowid, title, body, path) VALUES (new.id, new.title, new.body, new.path);
END;`

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

// migrateSchema applies incremental migrations for existing databases.
// New databases get the latest DDL directly; this handles upgrades.
func migrateSchema(db *sql.DB) error {
	ver, err := GetSchemaVersion(db)
	if err != nil {
		// schema_version table doesn't exist yet — fresh DB, no migration needed.
		return nil
	}

	if ver < 2 {
		// v2: add revoked_at column to api_tokens for soft revocation.
		_, err := db.Exec("ALTER TABLE api_tokens ADD COLUMN revoked_at TEXT")
		if err != nil {
			// Column may already exist (e.g., fresh DB with latest DDL).
			// SQLite returns "duplicate column name" — safe to ignore.
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("migrate v2 (add revoked_at): %w", err)
			}
		}
	}

	if ver < 3 {
		// v3: add OAuth tables for OAuth 2.0 authorization server support.
		oauthTables := []string{
			createOAuthClientsTable,
			createOAuthAuthCodesTable,
			createOAuthAccessTokensTable,
			createOAuthRefreshTokensTable,
		}
		for _, ddl := range oauthTables {
			if _, err := db.Exec(ddl); err != nil {
				// Tables may already exist (e.g., fresh DB with latest DDL).
				if !isTableExistsError(err) {
					return fmt.Errorf("migrate v3 (oauth tables): %w", err)
				}
			}
		}
	}

	if ver < 4 {
		// v4: drop FK constraints from oauth_access_tokens and oauth_refresh_tokens.
		// OAuth clients are stored in-memory (oauth.Store), not in SQLite, so the
		// FK constraint prevents inserting access tokens. Recreate without FK.
		migrations := []string{
			// Recreate oauth_access_tokens without FK
			`CREATE TABLE IF NOT EXISTS oauth_access_tokens_new (
				token TEXT PRIMARY KEY,
				client_id TEXT NOT NULL,
				scope TEXT,
				user_id TEXT,
				expires_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL
			)`,
			`INSERT OR IGNORE INTO oauth_access_tokens_new SELECT * FROM oauth_access_tokens`,
			`DROP TABLE oauth_access_tokens`,
			`ALTER TABLE oauth_access_tokens_new RENAME TO oauth_access_tokens`,
			// Recreate oauth_refresh_tokens without FK
			`CREATE TABLE IF NOT EXISTS oauth_refresh_tokens_new (
				token TEXT PRIMARY KEY,
				client_id TEXT NOT NULL,
				scope TEXT,
				user_id TEXT,
				expires_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL
			)`,
			`INSERT OR IGNORE INTO oauth_refresh_tokens_new SELECT * FROM oauth_refresh_tokens`,
			`DROP TABLE oauth_refresh_tokens`,
			`ALTER TABLE oauth_refresh_tokens_new RENAME TO oauth_refresh_tokens`,
			// Recreate indexes
			`CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_client ON oauth_access_tokens(client_id)`,
			`CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at)`,
		}
		for _, stmt := range migrations {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v4 (drop oauth FKs): %w", err)
			}
		}
	}

	if ver < 5 {
		// v5: add webhooks and webhook_deliveries tables for event hook system.
		webhookTables := []string{
			createWebhooksTable,
			createWebhookDeliveriesTable,
		}
		for _, ddl := range webhookTables {
			if _, err := db.Exec(ddl); err != nil {
				if !isTableExistsError(err) {
					return fmt.Errorf("migrate v5 (webhook tables): %w", err)
				}
			}
		}
	}

	return nil
}

// isDuplicateColumnError checks if an error is SQLite's "duplicate column name".
func isDuplicateColumnError(err error) bool {
	return err != nil && contains(err.Error(), "duplicate column name")
}

// isTableExistsError checks if an error is SQLite's "table already exists".
func isTableExistsError(err error) bool {
	return err != nil && contains(err.Error(), "already exists")
}

// contains is a simple substring check (avoids importing strings).
func contains(s, substr string) bool {
	return len(substr) <= len(s) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Schema initialization
// ---------------------------------------------------------------------------

// InitSchema creates all tables, indexes, FTS5 virtual table, and triggers.
// It is idempotent — safe to call multiple times.
func InitSchema(db *sql.DB) error {
	// Tables (order matters for foreign keys)
	tables := []string{
		createNotesTable,
		createLinksTable,
		createTagsTable,
		createEntryMetaTable,
		createGeneratedTasksTable,
		createSchemaVersionTable,
		createAPITokensTable,
		createOAuthClientsTable,
		createOAuthAuthCodesTable,
		createOAuthAccessTokensTable,
		createOAuthRefreshTokensTable,
		createWebhooksTable,
		createWebhookDeliveriesTable,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	// FTS5 virtual table
	if _, err := db.Exec(createFTS5Table); err != nil {
		return fmt.Errorf("create FTS5 table: %w", err)
	}

	// FTS5 sync triggers
	triggers := []string{
		createTriggerAfterInsert,
		createTriggerAfterDelete,
		createTriggerAfterUpdate,
	}
	for _, ddl := range triggers {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create trigger: %w", err)
		}
	}

	// Run migrations for existing databases (may drop/recreate tables).
	if err := migrateSchema(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}

	// Indexes (after migrations, so they apply to final table state)
	for _, ddl := range createIndexes {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// Set schema version (idempotent: INSERT OR REPLACE)
	if _, err := db.Exec(
		"INSERT OR REPLACE INTO schema_version (version) VALUES (?)",
		CurrentSchemaVersion,
	); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return nil
}

// GetSchemaVersion returns the highest schema version, or 0 if none set.
func GetSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}
	return version, nil
}

// SetSchemaVersion records a schema version.
func SetSchemaVersion(db *sql.DB, version int) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO schema_version (version) VALUES (?)",
		version,
	)
	if err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
}
