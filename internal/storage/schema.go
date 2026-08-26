package storage

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the latest schema version.
const CurrentSchemaVersion = 26

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
  scope TEXT NOT NULL DEFAULT 'admin:*',
  created_at TEXT DEFAULT (datetime('now')),
  last_used TEXT,
  revoked_at TEXT
);`

const createEventLogTable = `
CREATE TABLE IF NOT EXISTS event_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  dedup_key TEXT,
  source TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  processed_at TEXT
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

const createTaskClaimsTable = `
CREATE TABLE IF NOT EXISTS task_claims (
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  runner_id TEXT NOT NULL,
  claimed_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (project_id, task_id)
);`

const createTaskDispatchLeasesTable = `
CREATE TABLE IF NOT EXISTS task_dispatch_leases (
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  lease_id TEXT NOT NULL DEFAULT '',
  assigned_runner_id TEXT NOT NULL,
  assigned_machine_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  pushed_at INTEGER NOT NULL,
  acked_at INTEGER NOT NULL DEFAULT 0,
  rejected_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (project_id, task_id)
);`

const createTaskPlacementReasonsTable = `
CREATE TABLE IF NOT EXISTS task_placement_reasons (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  runner_id TEXT NOT NULL DEFAULT '',
  machine_id TEXT NOT NULL DEFAULT '',
  decision TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  required_labels TEXT NOT NULL DEFAULT '{}',
  runner_labels TEXT NOT NULL DEFAULT '{}',
  missing_labels TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL
);`

const createFeatureAssignmentsTable = `
CREATE TABLE IF NOT EXISTS feature_assignments (
  project_id TEXT NOT NULL,
  feature_id TEXT NOT NULL,
  runner_id TEXT NOT NULL,
  source TEXT NOT NULL,
  status TEXT NOT NULL,
  assigned_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (project_id, feature_id)
);`

const createRunnersTable = `
CREATE TABLE IF NOT EXISTS runners (
  runner_id TEXT PRIMARY KEY,
  machine_id TEXT DEFAULT '',
  hostname TEXT NOT NULL,
  labels TEXT DEFAULT '{}',
  executors TEXT DEFAULT '[]',
  capabilities TEXT DEFAULT '[]',
  dispatch_push INTEGER NOT NULL DEFAULT 0,
  workspace_roots TEXT DEFAULT '[]',
  projects TEXT DEFAULT '[]',
  resources TEXT DEFAULT '{}',
  capacity TEXT DEFAULT '{}',
  draining INTEGER NOT NULL DEFAULT 0,
  max_parallel INTEGER NOT NULL DEFAULT 1,
  feature_ids TEXT DEFAULT '',
  registered_at INTEGER NOT NULL,
  last_heartbeat INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'online'
);`

const createOpencodeInstancesTable = `
CREATE TABLE IF NOT EXISTS opencode_instances (
  instance_id TEXT PRIMARY KEY,
  runner_id TEXT NOT NULL,
  hostname TEXT DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'task',
  project_id TEXT DEFAULT '',
  task_id TEXT DEFAULT '',
  feature_id TEXT DEFAULT '',
  priority TEXT DEFAULT '',
  title TEXT DEFAULT '',
  workdir TEXT DEFAULT '',
  port INTEGER DEFAULT 0,
  pid INTEGER DEFAULT 0,
  session_ids TEXT DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'starting',
  executor TEXT DEFAULT 'opencode',
  agent TEXT DEFAULT '',
  model TEXT DEFAULT '',
  started_at INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0
);`

const createProjectPauseStateTable = `
CREATE TABLE IF NOT EXISTS project_pause_state (
  project_id TEXT PRIMARY KEY,
  tasks_paused INTEGER NOT NULL DEFAULT 0,
  automations_paused INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);`

const createRunnerPauseStateTable = `
CREATE TABLE IF NOT EXISTS runner_pause_state (
  runner_id TEXT PRIMARY KEY,
  paused INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);`

const createFeatureCascadeRootsTable = `
CREATE TABLE IF NOT EXISTS feature_cascade_roots (
  project_id        TEXT NOT NULL,
  root_feature_id   TEXT NOT NULL,
  requested_at      INTEGER NOT NULL,
  paused_at_request INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, root_feature_id)
);`

const createProjectPlacementTable = `
CREATE TABLE IF NOT EXISTS project_placement (
  project_id TEXT PRIMARY KEY,
  affinity TEXT NOT NULL DEFAULT 'soft',
  preferred_machines TEXT NOT NULL DEFAULT '[]',
  allowed_machines TEXT NOT NULL DEFAULT '[]',
  workspace_policy TEXT NOT NULL DEFAULT '',
  required_labels TEXT NOT NULL DEFAULT '{}',
  required_capabilities TEXT NOT NULL DEFAULT '[]',
  resource_requirements TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);`

const createBrainClientsTable = `
CREATE TABLE IF NOT EXISTS brain_clients (
  client_id TEXT PRIMARY KEY,
  kind TEXT DEFAULT '',
  host_id TEXT NOT NULL,
  hostname TEXT DEFAULT '',
  os TEXT DEFAULT '',
  arch TEXT DEFAULT '',
  username TEXT DEFAULT '',
  home_dir TEXT DEFAULT '',
  labels TEXT DEFAULT '{}',
  capabilities TEXT DEFAULT '[]',
  registered_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'online'
);`

const createBrainClientWorkspacesTable = `
CREATE TABLE IF NOT EXISTS brain_client_workspaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id TEXT NOT NULL,
  host_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  path TEXT NOT NULL,
  git_root TEXT DEFAULT '',
  git_common_dir TEXT DEFAULT '',
  git_worktree_main TEXT DEFAULT '',
  git_branch TEXT DEFAULT '',
  git_remote TEXT DEFAULT '',
  folder_name TEXT DEFAULT '',
  confidence TEXT DEFAULT '',
  resolution_source TEXT DEFAULT '',
  first_seen INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  UNIQUE(client_id, path)
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

const createNoteEmbeddingsTable = `
CREATE TABLE IF NOT EXISTS note_embeddings (
  note_id INTEGER NOT NULL,
  chunk_index INTEGER NOT NULL,
  embedding BLOB NOT NULL,
  PRIMARY KEY (note_id, chunk_index),
  FOREIGN KEY (note_id) REFERENCES notes(id) ON DELETE CASCADE
);`

const createNoteEmbeddingsMetaTable = `
CREATE TABLE IF NOT EXISTS note_embeddings_meta (
  note_id INTEGER NOT NULL,
  chunk_index INTEGER NOT NULL,
  project_id TEXT,
  type TEXT,
  status TEXT,
  feature_id TEXT,
  priority TEXT,
  embedding_indexed_at TEXT DEFAULT (datetime('now')),
  PRIMARY KEY (note_id, chunk_index),
  FOREIGN KEY (note_id) REFERENCES notes(id) ON DELETE CASCADE
);`

const createAttachmentsTable = `
CREATE TABLE IF NOT EXISTS attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  digest TEXT UNIQUE NOT NULL,
  size INTEGER NOT NULL,
  media_type TEXT DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);`

const createEntryAttachmentsTable = `
CREATE TABLE IF NOT EXISTS entry_attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  attachment_id INTEGER NOT NULL REFERENCES attachments(id) ON DELETE RESTRICT,
  role TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(note_id, attachment_id, role)
);`

const createAttachmentDerivedTable = `
CREATE TABLE IF NOT EXISTS attachment_derived (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  attachment_id INTEGER NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'text',
  status TEXT NOT NULL DEFAULT '',
  content_type TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(attachment_id, kind)
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
	"CREATE INDEX IF NOT EXISTS idx_event_log_type_created ON event_log(event_type, created_at);",
	"CREATE UNIQUE INDEX IF NOT EXISTS idx_event_log_dedup_key ON event_log(dedup_key) WHERE dedup_key IS NOT NULL;",
	// Task claims indexes
	"CREATE INDEX IF NOT EXISTS idx_claims_runner ON task_claims(runner_id);",
	"CREATE INDEX IF NOT EXISTS idx_claims_expires ON task_claims(expires_at);",
	// Task dispatch lease indexes
	"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_runner ON task_dispatch_leases(assigned_runner_id);",
	"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_machine ON task_dispatch_leases(assigned_machine_id);",
	"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_state ON task_dispatch_leases(state);",
	"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_expires ON task_dispatch_leases(expires_at);",
	// Task placement reason indexes
	"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_task ON task_placement_reasons(project_id, task_id);",
	"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_runner ON task_placement_reasons(runner_id);",
	"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_decision ON task_placement_reasons(decision);",
	"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_created ON task_placement_reasons(created_at);",
	// Feature assignment indexes
	"CREATE INDEX IF NOT EXISTS idx_feature_assignments_runner ON feature_assignments(runner_id);",
	"CREATE INDEX IF NOT EXISTS idx_feature_assignments_project ON feature_assignments(project_id);",
	"CREATE INDEX IF NOT EXISTS idx_feature_assignments_status ON feature_assignments(status);",
	// Runners indexes
	"CREATE INDEX IF NOT EXISTS idx_runners_status ON runners(status);",
	// OpenCode instance indexes
	"CREATE INDEX IF NOT EXISTS idx_opencode_instances_runner ON opencode_instances(runner_id);",
	"CREATE INDEX IF NOT EXISTS idx_opencode_instances_task ON opencode_instances(project_id, task_id);",
	"CREATE INDEX IF NOT EXISTS idx_project_placement_affinity ON project_placement(affinity);",
	"CREATE INDEX IF NOT EXISTS idx_project_pause_state_tasks ON project_pause_state(tasks_paused);",
	"CREATE INDEX IF NOT EXISTS idx_project_pause_state_automations ON project_pause_state(automations_paused);",
	"CREATE INDEX IF NOT EXISTS idx_runner_pause_state_paused ON runner_pause_state(paused);",
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
	// Note embeddings indexes
	"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_project ON note_embeddings_meta(project_id);",
	"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_type ON note_embeddings_meta(type);",
	"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_status ON note_embeddings_meta(status);",
	"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_feature ON note_embeddings_meta(feature_id);",
	"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_priority ON note_embeddings_meta(priority);",
	// Attachment indexes
	"CREATE INDEX IF NOT EXISTS idx_attachments_digest ON attachments(digest);",
	"CREATE INDEX IF NOT EXISTS idx_entry_attachments_note ON entry_attachments(note_id);",
	"CREATE INDEX IF NOT EXISTS idx_entry_attachments_attachment ON entry_attachments(attachment_id);",
	"CREATE UNIQUE INDEX IF NOT EXISTS idx_entry_attachments_note_attachment_role ON entry_attachments(note_id, attachment_id, role);",
	"CREATE INDEX IF NOT EXISTS idx_attachment_derived_attachment ON attachment_derived(attachment_id);",
	"CREATE INDEX IF NOT EXISTS idx_attachment_derived_status ON attachment_derived(status);",
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
		// v5: add event_log table for durable event delivery.
		if _, err := db.Exec(createEventLogTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v5 (event_log table): %w", err)
			}
		}
		eventIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_event_log_type_created ON event_log(event_type, created_at)",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_event_log_dedup_key ON event_log(dedup_key) WHERE dedup_key IS NOT NULL",
		}
		for _, stmt := range eventIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v5 (event_log indexes): %w", err)
			}
		}
	}

	if ver < 6 {
		// v6: add task_claims table for persistent lease-based task claims (multi-runner support).
		if _, err := db.Exec(createTaskClaimsTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v6 (task_claims table): %w", err)
			}
		}
		claimIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_claims_runner ON task_claims(runner_id)",
			"CREATE INDEX IF NOT EXISTS idx_claims_expires ON task_claims(expires_at)",
		}
		for _, stmt := range claimIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v6 (task_claims indexes): %w", err)
			}
		}
	}

	if ver < 7 {
		// v7: add runners table for runner registration (horizontal scaling).
		if _, err := db.Exec(createRunnersTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v7 (runners table): %w", err)
			}
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_runners_status ON runners(status)"); err != nil {
			return fmt.Errorf("migrate v7 (runners index): %w", err)
		}
	}

	if ver < 8 {
		// v8: add scope column to api_tokens for scoped authorization.
		// Existing tokens default to 'admin:*' (full access) for backward compatibility.
		// Only alter if api_tokens exists (it may not in partial schema scenarios).
		var tblName string
		tblErr := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='api_tokens'").Scan(&tblName)
		if tblErr == nil {
			_, err := db.Exec("ALTER TABLE api_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT 'admin:*'")
			if err != nil {
				if !isDuplicateColumnError(err) {
					return fmt.Errorf("migrate v8 (add scope to api_tokens): %w", err)
				}
			}
		}
	}

	if ver < 9 {
		// v9: add webhooks and webhook_deliveries tables for event hook system.
		webhookTables := []string{
			createWebhooksTable,
			createWebhookDeliveriesTable,
		}
		for _, ddl := range webhookTables {
			if _, err := db.Exec(ddl); err != nil {
				if !isTableExistsError(err) {
					return fmt.Errorf("migrate v9 (webhook tables): %w", err)
				}
			}
		}
		webhookIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_webhooks_enabled ON webhooks(enabled)",
			"CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id)",
			"CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at)",
		}
		for _, stmt := range webhookIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v9 (webhook indexes): %w", err)
			}
		}
	}

	if ver < 10 {
		// v10: add feature_assignments table for durable server-enforced feature affinity.
		if _, err := db.Exec(createFeatureAssignmentsTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v10 (feature_assignments table): %w", err)
			}
		}
		featureAssignmentIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_feature_assignments_runner ON feature_assignments(runner_id)",
			"CREATE INDEX IF NOT EXISTS idx_feature_assignments_project ON feature_assignments(project_id)",
			"CREATE INDEX IF NOT EXISTS idx_feature_assignments_status ON feature_assignments(status)",
		}
		for _, stmt := range featureAssignmentIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v10 (feature_assignments indexes): %w", err)
			}
		}
	}

	if ver < 11 {
		// v11: persist runner capabilities for server-side capability routing.
		var tblName string
		tblErr := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='runners'").Scan(&tblName)
		if tblErr == nil {
			_, err := db.Exec("ALTER TABLE runners ADD COLUMN capabilities TEXT DEFAULT '[]'")
			if err != nil {
				if !isDuplicateColumnError(err) {
					return fmt.Errorf("migrate v11 (add runner capabilities): %w", err)
				}
			}
		}
	}

	if ver < 12 {
		// v12: add note_embeddings and note_embeddings_meta tables for embedding-based search.
		if err := ensureNoteEmbeddingsTable(db); err != nil {
			return fmt.Errorf("migrate v12 (note_embeddings table): %w", err)
		}
		if _, err := db.Exec(createNoteEmbeddingsMetaTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v12 (embedding meta table): %w", err)
			}
		}
		embeddingIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_project ON note_embeddings_meta(project_id)",
			"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_type ON note_embeddings_meta(type)",
			"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_status ON note_embeddings_meta(status)",
			"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_feature ON note_embeddings_meta(feature_id)",
			"CREATE INDEX IF NOT EXISTS idx_note_embeddings_meta_priority ON note_embeddings_meta(priority)",
		}
		for _, stmt := range embeddingIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v12 (embedding indexes): %w", err)
			}
		}
	}

	if ver < 13 {
		// v13: add attachment metadata and entry-reference tables.
		attachmentTables := []string{
			createAttachmentsTable,
			createEntryAttachmentsTable,
		}
		for _, ddl := range attachmentTables {
			if _, err := db.Exec(ddl); err != nil {
				if !isTableExistsError(err) {
					return fmt.Errorf("migrate v13 (attachment tables): %w", err)
				}
			}
		}
		attachmentIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_attachments_digest ON attachments(digest)",
			"CREATE INDEX IF NOT EXISTS idx_entry_attachments_note ON entry_attachments(note_id)",
			"CREATE INDEX IF NOT EXISTS idx_entry_attachments_attachment ON entry_attachments(attachment_id)",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_entry_attachments_note_attachment_role ON entry_attachments(note_id, attachment_id, role)",
		}
		for _, stmt := range attachmentIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v13 (attachment indexes): %w", err)
			}
		}
	}

	if ver < 14 {
		// v14: add derived attachment extraction status/text table.
		if _, err := db.Exec(createAttachmentDerivedTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v14 (attachment derived table): %w", err)
			}
		}
		attachmentDerivedIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_attachment_derived_attachment ON attachment_derived(attachment_id)",
			"CREATE INDEX IF NOT EXISTS idx_attachment_derived_status ON attachment_derived(status)",
		}
		for _, stmt := range attachmentDerivedIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v14 (attachment derived indexes): %w", err)
			}
		}
	}

	if ver < 15 {
		// v15: register Brain clients and observed workspaces for project context resolution.
		clientTables := []string{
			createBrainClientsTable,
			createBrainClientWorkspacesTable,
		}
		for _, ddl := range clientTables {
			if _, err := db.Exec(ddl); err != nil {
				if !isTableExistsError(err) {
					return fmt.Errorf("migrate v15 (brain client tables): %w", err)
				}
			}
		}
		clientIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_brain_clients_host ON brain_clients(host_id)",
			"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_project ON brain_client_workspaces(project_id)",
			"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_host_path ON brain_client_workspaces(host_id, path)",
			"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_git_remote ON brain_client_workspaces(git_remote)",
		}
		for _, stmt := range clientIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v15 (brain client indexes): %w", err)
			}
		}
	}

	if ver < 16 {
		// v16: add opencode_instances table for the remote-control instance registry.
		if _, err := db.Exec(createOpencodeInstancesTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v16 (opencode_instances table): %w", err)
			}
		}
		instanceIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_opencode_instances_runner ON opencode_instances(runner_id)",
			"CREATE INDEX IF NOT EXISTS idx_opencode_instances_task ON opencode_instances(project_id, task_id)",
		}
		for _, stmt := range instanceIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v16 (opencode_instances indexes): %w", err)
			}
		}
	}

	if exists, err := tableExists(db, "opencode_instances"); err != nil {
		return fmt.Errorf("migrate opencode_instances metadata (inspect table): %w", err)
	} else if exists {
		columns := []struct {
			name string
			ddl  string
		}{
			{"feature_id", "ALTER TABLE opencode_instances ADD COLUMN feature_id TEXT DEFAULT ''"},
			{"priority", "ALTER TABLE opencode_instances ADD COLUMN priority TEXT DEFAULT ''"},
			{"agent", "ALTER TABLE opencode_instances ADD COLUMN agent TEXT DEFAULT ''"},
			{"model", "ALTER TABLE opencode_instances ADD COLUMN model TEXT DEFAULT ''"},
		}
		for _, column := range columns {
			if err := ensureTableColumn(db, "opencode_instances", column.name, column.ddl); err != nil {
				return fmt.Errorf("migrate opencode_instances metadata (add %s): %w", column.name, err)
			}
		}
	}

	if ver < 18 {
		// v18: add explicit runner dispatch metadata for push scheduling.
		var tblName string
		tblErr := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='runners'").Scan(&tblName)
		if tblErr == nil {
			columns := []struct {
				name string
				ddl  string
			}{
				{"machine_id", "ALTER TABLE runners ADD COLUMN machine_id TEXT DEFAULT ''"},
				{"dispatch_push", "ALTER TABLE runners ADD COLUMN dispatch_push INTEGER NOT NULL DEFAULT 0"},
				{"workspace_roots", "ALTER TABLE runners ADD COLUMN workspace_roots TEXT DEFAULT '[]'"},
				{"projects", "ALTER TABLE runners ADD COLUMN projects TEXT DEFAULT '[]'"},
				{"resources", "ALTER TABLE runners ADD COLUMN resources TEXT DEFAULT '{}'"},
				{"capacity", "ALTER TABLE runners ADD COLUMN capacity TEXT DEFAULT '{}'"},
				{"draining", "ALTER TABLE runners ADD COLUMN draining INTEGER NOT NULL DEFAULT 0"},
			}
			for _, column := range columns {
				if _, err := db.Exec(column.ddl); err != nil {
					if !isDuplicateColumnError(err) {
						return fmt.Errorf("migrate v18 (add runner %s): %w", column.name, err)
					}
				}
			}
		}
	}

	if ver < 20 {
		// v20: add placement reason storage for scheduler decisions.
		if _, err := db.Exec(createTaskPlacementReasonsTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v20 (task_placement_reasons table): %w", err)
			}
		}
		placementReasonIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_task ON task_placement_reasons(project_id, task_id)",
			"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_runner ON task_placement_reasons(runner_id)",
			"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_decision ON task_placement_reasons(decision)",
			"CREATE INDEX IF NOT EXISTS idx_task_placement_reasons_created ON task_placement_reasons(created_at)",
		}
		for _, stmt := range placementReasonIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v20 (task_placement_reasons indexes): %w", err)
			}
		}
	}

	if ver < 22 {
		// v22: persist per-project pause state for tasks and automations.
		if _, err := db.Exec(createProjectPauseStateTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v22 (project_pause_state table): %w", err)
			}
		}
		pauseIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_project_pause_state_tasks ON project_pause_state(tasks_paused)",
			"CREATE INDEX IF NOT EXISTS idx_project_pause_state_automations ON project_pause_state(automations_paused)",
		}
		for _, stmt := range pauseIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v22 (project_pause_state indexes): %w", err)
			}
		}
	}

	if ver < 23 {
		// v23: persist the runner-scoped pause dial.
		//
		// It lives in its own table rather than as a `runners` column
		// because the runners row is DELETED on deregister — and
		// `brain runner stop` deregisters. A column would mean a routine
		// stop/start silently resumed a runner an operator had paused.
		// Keyed by runner_id, which is stable across restarts.
		if _, err := db.Exec(createRunnerPauseStateTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v23 (runner_pause_state table): %w", err)
			}
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_runner_pause_state_paused ON runner_pause_state(paused)"); err != nil {
			return fmt.Errorf("migrate v23 (runner_pause_state index): %w", err)
		}
	}

	if ver < 24 {
		// v24: persist the ROOT of a manual "run feature + dependents"
		// request.
		//
		// Only the root is stored, never the member list. The closure is
		// derived from feature_depends_on, so a stored member set would go
		// stale the moment someone edits the graph — and the server would
		// then be dispatching a chain that no longer matches what is
		// declared. Re-deriving from the root on every sweep also means a
		// feature whose tasks are generated mid-run (feature-checkout
		// follow-ups, goal-generated work) joins the chain instead of being
		// invisible because it had no tasks at click time.
		//
		// paused_at_request captures whether the project's task dial was
		// already off when the user asked. Propagation force-dispatches
		// past a pause that was already on — that is the isolate workflow —
		// but a pause applied AFTER the click stops the chain spreading
		// into features that have not started.
		if _, err := db.Exec(createFeatureCascadeRootsTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v24 (feature_cascade_roots table): %w", err)
			}
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_feature_cascade_roots_project ON feature_cascade_roots(project_id)"); err != nil {
			return fmt.Errorf("migrate v24 (feature_cascade_roots index): %w", err)
		}
	}

	if ver < 25 {
		// v25: force a re-extraction of links for the notes whose link rows
		// this build changes.
		//
		// Two fixes landed together: [[wiki-links]] are now parsed at all, and
		// link syntax inside code fences and inline code is no longer treated
		// as a link. Neither reaches content already on disk, because the boot
		// indexer (IndexChanged) skips any file whose checksum still matches
		// the row — so a brain full of wiki-links would stay unlinked until
		// each file happened to be edited.
		//
		// Nulling the checksum marks a note as "changed" for exactly one pass;
		// the next IndexChanged re-parses it from disk and rewrites its links
		// through the normal path. Scoped to the notes that can actually
		// differ — those containing "[[" and those that already produced link
		// rows — because the alternative, clearing every checksum, would
		// re-index the whole brain behind a single SQLite connection and
		// stall every read for the duration.
		//
		// SQLite LIKE has no character-class metacharacters, so '%[[%' is a
		// literal match for a doubled bracket.
		notesExist, err := tableExists(db, "notes")
		if err != nil {
			return fmt.Errorf("migrate v25 (inspect notes): %w", err)
		}
		linksExist, err := tableExists(db, "links")
		if err != nil {
			return fmt.Errorf("migrate v25 (inspect links): %w", err)
		}
		if notesExist && linksExist {
			if _, err := db.Exec(`UPDATE notes SET checksum = NULL
				WHERE body LIKE '%[[%'
				   OR id IN (SELECT DISTINCT source_id FROM links)`); err != nil {
				return fmt.Errorf("migrate v25 (invalidate checksums for link re-extraction): %w", err)
			}
		}
	}

	if ver < 26 {
		// v26: same idea as v25, for HTML comments.
		//
		// v25 assumed the placeholder link targets in the graph ("pattern-id",
		// "entry-id", "report-id") came from fenced code examples. They did
		// not: the plan-template entries keep their example links inside HTML
		// comments — "<!-- Link to patterns: [Pattern Name](pattern-id) -->" —
		// which v25's fence masking never touched, so those rows survived the
		// v25 backfill unchanged. ExtractLinks now masks comments too, and
		// these notes need one more re-extraction pass to drop the rows.
		//
		// Scoped the same way and to a similar size (443 notes in production
		// against 72,869), so it stays a short pass rather than a full
		// re-index behind SQLite's single connection.
		notesExist, err := tableExists(db, "notes")
		if err != nil {
			return fmt.Errorf("migrate v26 (inspect notes): %w", err)
		}
		linksExist, err := tableExists(db, "links")
		if err != nil {
			return fmt.Errorf("migrate v26 (inspect links): %w", err)
		}
		if notesExist && linksExist {
			if _, err := db.Exec(`UPDATE notes SET checksum = NULL
				WHERE body LIKE '%<!--%'
				   OR id IN (SELECT DISTINCT source_id FROM links)`); err != nil {
				return fmt.Errorf("migrate v26 (invalidate checksums for comment re-extraction): %w", err)
			}
		}
	}

	if ver < 21 {
		// v21: add stable lease IDs to dispatch leases for ack/reject validation.
		if exists, err := tableExists(db, "task_dispatch_leases"); err != nil {
			return fmt.Errorf("migrate v21 (inspect task_dispatch_leases): %w", err)
		} else if exists {
			hasLeaseID, err := tableColumnExists(db, "task_dispatch_leases", "lease_id")
			if err != nil {
				return fmt.Errorf("migrate v21 (inspect lease_id): %w", err)
			}
			if !hasLeaseID {
				if _, err := db.Exec("ALTER TABLE task_dispatch_leases ADD COLUMN lease_id TEXT NOT NULL DEFAULT ''"); err != nil {
					if !isDuplicateColumnError(err) {
						return fmt.Errorf("migrate v21 (add lease_id): %w", err)
					}
				}
				if _, err := db.Exec("UPDATE task_dispatch_leases SET lease_id = 'legacy-' || project_id || '-' || task_id WHERE lease_id = ''"); err != nil {
					return fmt.Errorf("migrate v21 (backfill lease_id): %w", err)
				}
			}
		}
	}

	if ver < 19 {
		// v19: add Brain-owned task dispatch leases for push scheduling.
		if _, err := db.Exec(createTaskDispatchLeasesTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v19 (task_dispatch_leases table): %w", err)
			}
		}
		dispatchLeaseIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_runner ON task_dispatch_leases(assigned_runner_id)",
			"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_machine ON task_dispatch_leases(assigned_machine_id)",
			"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_state ON task_dispatch_leases(state)",
			"CREATE INDEX IF NOT EXISTS idx_task_dispatch_leases_expires ON task_dispatch_leases(expires_at)",
		}
		for _, stmt := range dispatchLeaseIndexes {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate v19 (task_dispatch_leases indexes): %w", err)
			}
		}
	}

	if ver < 17 {
		// v17: add Brain-owned project placement metadata for scheduling policy.
		if _, err := db.Exec(createProjectPlacementTable); err != nil {
			if !isTableExistsError(err) {
				return fmt.Errorf("migrate v17 (project_placement table): %w", err)
			}
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_project_placement_affinity ON project_placement(affinity)"); err != nil {
			return fmt.Errorf("migrate v17 (project_placement indexes): %w", err)
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

func ensureNoteEmbeddingsTable(db *sql.DB) error {
	if exists, err := tableExists(db, "note_embeddings"); err != nil {
		return fmt.Errorf("inspect note_embeddings: %w", err)
	} else if exists {
		hasPath, err := tableColumnExists(db, "note_embeddings", "path")
		if err != nil {
			return fmt.Errorf("inspect note_embeddings.path: %w", err)
		}
		if hasPath {
			if _, err := db.Exec("DROP TABLE note_embeddings"); err != nil {
				return fmt.Errorf("drop legacy note_embeddings: %w", err)
			}
		}
	}
	if _, err := db.Exec(createNoteEmbeddingsTable); err != nil {
		if !isTableExistsError(err) {
			return err
		}
	}
	return nil
}

func ensureTableColumn(db *sql.DB, table, column, ddl string) error {
	exists, err := tableColumnExists(db, table, column)
	if err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if exists {
		return nil
	}
	if _, err := db.Exec(ddl); err != nil && !isDuplicateColumnError(err) {
		return err
	}
	return nil
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func tableColumnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
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
		createEventLogTable,
		createOAuthClientsTable,
		createOAuthAuthCodesTable,
		createOAuthAccessTokensTable,
		createOAuthRefreshTokensTable,
		createTaskClaimsTable,
		createTaskDispatchLeasesTable,
		createTaskPlacementReasonsTable,
		createFeatureAssignmentsTable,
		createRunnersTable,
		createOpencodeInstancesTable,
		createProjectPlacementTable,
		createProjectPauseStateTable,
		createRunnerPauseStateTable,
		createWebhooksTable,
		createWebhookDeliveriesTable,
		createNoteEmbeddingsMetaTable,
		createAttachmentsTable,
		createEntryAttachmentsTable,
		createAttachmentDerivedTable,
		createFeatureCascadeRootsTable,
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
	if err := ensureNoteEmbeddingsTable(db); err != nil {
		return fmt.Errorf("ensure note embeddings table: %w", err)
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
