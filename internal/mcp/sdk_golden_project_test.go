package mcp_test

import (
	"testing"
)

// TestGolden_ProjectReaderSyncTools covers project_tools.go, reader_tools.go
// and sync_tools.go on a dedicated server: client registrations and browser
// sync devices are server-wide. context_get makes no API call (it renders the
// process's ambient context) and is not recorded here.
func TestGolden_ProjectReaderSyncTools(t *testing.T) {
	api := dedicatedAPI(t)
	g := newGoldenAt(t, "project_reader_sync_tools", api)
	g.scrubRe(`"snapshot":"[0-9a-f]{64}"`, `"snapshot":"<SNAPSHOT>"`)
	g.scrubRe(`"command_id":"[0-9a-f]{32}"`, `"command_id":"<COMMAND>"`)
	g.scrubRe(`"id":"[0-9a-f]{32}"`, `"id":"<COMMAND>"`)
	g.scrubRe(`"server_revision":"[0-9a-f]{64}"`, `"server_revision":"<REVISION>"`)

	g.call("resolve missing client", "context_resolve", map[string]any{"host_id": "machine_00000001"})
	g.call("resolve missing host", "context_resolve", map[string]any{"client_id": "c-1"})
	g.call("resolve minimal", "context_resolve", map[string]any{"client_id": "c-1", "host_id": "machine_00000001"})
	g.call("resolve full", "context_resolve", map[string]any{
		"client_id": "c-2", "host_id": "machine_00000002", "kind": "opencode", "hostname": "box", "os": "darwin", "arch": "arm64",
		"username": "me", "home_dir": "/Users/me", "labels": map[string]any{"zone": "lab", "gpu": true}, "capabilities": []string{"git", "docker"},
		"path": "/Users/me/projects/brain-api", "git_root": "/Users/me/projects/brain-api", "git_common_dir": "/Users/me/projects/brain-api/.git",
		"git_worktree_main": "/Users/me/projects/brain-api", "git_branch": "main", "git_remote": "https://github.com/huynle/brain-api.git", "folder_name": "brain-api",
	})

	g.call("placement missing project", "project_placement_get", nil)
	g.call("placement default", "project_placement_get", map[string]any{"project": "place-me"})
	g.call("placement put", "project_placement_put", map[string]any{
		"project": "place-me", "affinity": "soft", "preferred_machines": []string{"m1", "m2"}, "allowed_machines": []string{"m1", "m2", "m3"},
		"workspace_policy": "worktree", "required_labels": map[string]any{"zone": "lab"}, "required_capabilities": []string{"git"},
		"resources": map[string]any{"memory_mb": 2048, "cpu": "2"},
	})
	g.call("placement get after put", "project_placement_get", map[string]any{"project": "place-me"})
	g.call("placement put invalid affinity", "project_placement_put", map[string]any{"project": "place-me", "affinity": "sometimes"})
	g.call("placement put minimal", "project_placement_put", map[string]any{"project": "place me/2", "affinity": "none"})
	g.call("placement put missing project", "project_placement_put", map[string]any{"affinity": "strict"})

	entry := entryFields(t, api, map[string]any{"type": "scratch", "title": "Readable note", "content": "body text", "project": "reader"})
	g.bind("ENTRY", entry)
	g.call("reader by id", "reader_url", map[string]any{"path": entry})
	g.call("reader by path", "reader_url", map[string]any{"path": " projects/reader/scratch/" + entry + ".md "})
	g.call("reader base override", "reader_url", map[string]any{"path": entry, "base_url": "https://brain.example.test/"})
	g.call("reader base invalid", "reader_url", map[string]any{"path": entry, "base_url": "https://u:p@brain.example.test"})
	g.call("reader base with path", "reader_url", map[string]any{"path": entry, "base_url": "https://brain.example.test/app"})
	g.call("reader missing path", "reader_url", map[string]any{"path": "  "})
	g.call("reader unknown", "reader_url", map[string]any{"path": "zzzzzzzz"})
	g.call("reader unknown path", "reader_url", map[string]any{"path": "projects/reader/scratch/missing file.md"})

	g.call("sync status empty", "sync_status", nil)
	g.call("sync diff missing args", "sync_diff", map[string]any{"device_id": "golden-device-0001"})
	g.call("sync diff unknown device", "sync_diff", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1"})
	g.call("sync reconcile unknown device", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": "s", "action": "discard"})

	synced := entryFields(t, api, map[string]any{"type": "scratch", "title": "Synced note", "content": "server body", "project": "sync"})
	g.bind("SYNCED", synced)
	apiJSON(t, "POST", api+"/api/v1/sync/devices/golden-device-0001/report", map[string]any{
		"reported_online": true, "syncing": false, "ready": true, "cursor": 7, "epoch": "epoch-1", "cache_mode": "recent", "cached_entries": 3,
		"pending": []map[string]any{
			{"id": "op-1", "path": "projects/sync/scratch/" + synced + ".md", "method": "PATCH", "revision": "r0", "raw": "---\ntitle: Synced note\n---\nbrowser body", "error": "revision conflict"},
			{"id": "op-2", "path": "projects/sync/scratch/missing.md", "method": "PATCH", "revision": "r9", "raw": "---\ntitle: Gone\n---\nx", "error": "timeout", "failure": "uncertain"},
		},
	}, nil)

	g.call("sync status", "sync_status", nil)
	diff := g.call("sync diff", "sync_diff", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1"})
	snapshot := field(t, diff, `"snapshot":"([0-9a-f]{64})"`)
	missing := g.call("sync diff missing entry", "sync_diff", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-2"})
	missingSnapshot := field(t, missing, `"snapshot":"([0-9a-f]{64})"`)
	g.call("sync diff unknown op", "sync_diff", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-9"})
	g.call("sync reconcile invalid action", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": snapshot, "action": "keep"})
	g.call("sync reconcile merge without raw", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": snapshot, "action": "merge"})
	g.call("sync reconcile stale", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": "stale", "action": "rebase"})
	g.call("sync reconcile merge invalid raw", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": snapshot, "action": "merge", "raw": "no frontmatter"})
	g.call("sync reconcile uncertain rebase", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-2", "snapshot": missingSnapshot, "action": "rebase"})
	g.call("sync reconcile discard", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": snapshot, "action": "discard"})
	g.call("sync reconcile pending", "sync_reconcile", map[string]any{"device_id": "golden-device-0001", "operation_id": "op-1", "snapshot": snapshot, "action": "discard"})
	g.call("sync status after", "sync_status", nil)

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead resolve", "context_resolve", map[string]any{"client_id": "c", "host_id": "h"})
	g.callAt(dead, "dead placement get", "project_placement_get", map[string]any{"project": "p q"})
	g.callAt(dead, "dead placement put", "project_placement_put", map[string]any{"project": "p/q"})
	g.callAt(dead, "dead reader", "reader_url", map[string]any{"path": "projects/p q/note/a.md"})
	g.callAt(dead, "dead sync status", "sync_status", nil)
	g.callAt(dead, "dead sync diff", "sync_diff", map[string]any{"device_id": "d 1", "operation_id": "o/1"})
	g.callAt(dead, "dead sync reconcile", "sync_reconcile", map[string]any{"device_id": "d", "operation_id": "o", "snapshot": "s", "action": "discard"})
	g.check()
}
