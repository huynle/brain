package storage

// Sync history is authoritative: these tables deliberately do NOT reference live
// notes. Deleted paths, receipts and devices must outlive index rebuilds/deletes.
// seq remains a global SQLite physical identity; tenant predicates bound its use.
func successorSyncDefinitions() map[string]string {
	return map[string]string{
		"entry_sync_devices":                `CREATE TABLE entry_sync_devices (tenant_id TEXT NOT NULL REFERENCES tenants(id), id TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(tenant_id,id))`,
		"entry_sync_identity":               `CREATE TABLE entry_sync_identity (tenant_id TEXT NOT NULL REFERENCES tenants(id), id INTEGER NOT NULL CHECK(id=1), epoch TEXT NOT NULL, PRIMARY KEY(tenant_id,id))`,
		"entry_sync_changes":                `CREATE TABLE entry_sync_changes (tenant_id TEXT NOT NULL REFERENCES tenants(id), seq INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, UNIQUE(tenant_id,path))`,
		"entry_sync_operations":             `CREATE TABLE entry_sync_operations (tenant_id TEXT NOT NULL REFERENCES tenants(id), id TEXT NOT NULL, hash TEXT NOT NULL, status INTEGER NOT NULL DEFAULT 0, body TEXT NOT NULL DEFAULT '', PRIMARY KEY(tenant_id,id))`,
		"entry_sync_changes_owner_sequence": `CREATE INDEX entry_sync_changes_owner_sequence ON entry_sync_changes(tenant_id,seq)`,
	}
}

func successorSyncTriggers() map[string]string {
	return map[string]string{
		// REPLACE suppresses implicit DELETE triggers when recursive_triggers=0.
		// Before-insert observation preserves the old-path cursor position. An
		// ignored insert (or undefined implicit NEW.id=-1 collision) may produce
		// an extra change, but readers return the still-live payload, never a false
		// tombstone. Cross-owner attempts ABORT atomically through the FTS guards.
		"entry_sync_replace": `CREATE TRIGGER entry_sync_replace BEFORE INSERT ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(tenant_id,path) SELECT tenant_id,path FROM notes WHERE tenant_id=new.tenant_id AND id=new.id AND path IS NOT new.path;
END`,
		"entry_sync_insert": `CREATE TRIGGER entry_sync_insert AFTER INSERT ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(tenant_id,path) VALUES (new.tenant_id,new.path);
END`,
		"entry_sync_update": `CREATE TRIGGER entry_sync_update AFTER UPDATE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(tenant_id,path) VALUES (old.tenant_id,old.path);
 INSERT OR REPLACE INTO entry_sync_changes(tenant_id,path) VALUES (new.tenant_id,new.path);
END`,
		"entry_sync_delete": `CREATE TRIGGER entry_sync_delete AFTER DELETE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(tenant_id,path) VALUES (old.tenant_id,old.path);
END`,
	}
}
