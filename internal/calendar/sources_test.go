package calendar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleToken = "TOKEN-sources-7731"

func sampleRecord(name string, now time.Time) sourceRecord {
	return sourceRecord{
		Name:      name,
		URL:       "https://calendar.example/feed/" + sampleToken + "/cal.ics",
		Poll:      5 * time.Minute,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func sourcesDir(dataDir string) string { return filepath.Join(dataDir, "calendars", "sources") }

func TestSourceStoreWritesPrivateFileAndDirectory(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	if err := newSourceStore(dataDir).save(sampleRecord("personal", now)); err != nil {
		t.Fatalf("save: %v", err)
	}

	dirInfo, err := os.Stat(sourcesDir(dataDir))
	if err != nil {
		t.Fatalf("stat sources dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("sources dir mode %o, want 0700", mode)
	}
	fileInfo, err := os.Stat(filepath.Join(sourcesDir(dataDir), "personal.json"))
	if err != nil {
		t.Fatalf("stat source file: %v", err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("source file mode %o, want 0600", mode)
	}
}

func TestSourceStoreFileHoldsDefinitionOnly(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	if err := newSourceStore(dataDir).save(sampleRecord("personal", now)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sourcesDir(dataDir), "personal.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name": "personal"`, `"url": "https://calendar.example/feed/` + sampleToken, `"poll": "5m0s"`, `"created_at"`, `"updated_at"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("source file lacks %q:\n%s", want, data)
		}
	}
}

func TestSourceStoreOverwriteReplacesRecordWithoutTempLeftovers(t *testing.T) {
	dataDir := t.TempDir()
	store := newSourceStore(dataDir)
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	rec := sampleRecord("personal", now)
	if err := store.save(rec); err != nil {
		t.Fatal(err)
	}
	rec.URL = "https://calendar.example/feed/NEW-TOKEN/cal.ics"
	rec.Poll = 10 * time.Minute
	rec.UpdatedAt = now.Add(time.Hour)
	if err := store.save(rec); err != nil {
		t.Fatal(err)
	}

	loaded, issues := newSourceStore(dataDir).loadAll()
	if len(issues) != 0 || len(loaded) != 1 {
		t.Fatalf("loadAll after overwrite: records=%+v issues=%+v", loaded, issues)
	}
	if loaded[0].URL != rec.URL || loaded[0].Poll != 10*time.Minute {
		t.Fatalf("overwrite not applied: %+v", loaded[0])
	}
	entries, err := os.ReadDir(sourcesDir(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "personal.json" {
			t.Fatalf("leftover file after atomic write: %s", e.Name())
		}
	}
}

func TestSourceStoreRemoveIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	store := newSourceStore(dataDir)
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	if err := store.save(sampleRecord("personal", now)); err != nil {
		t.Fatal(err)
	}
	if err := store.remove("personal"); err != nil {
		t.Fatalf("remove existing: %v", err)
	}
	if err := store.remove("personal"); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
	if loaded, _ := store.loadAll(); len(loaded) != 0 {
		t.Fatalf("removed source still loads: %+v", loaded)
	}
}

func TestSourceStoreReloadsAfterRestartSortedByName(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	first := newSourceStore(dataDir)
	for _, name := range []string{"zeta", "alpha", "mid-1"} {
		if err := first.save(sampleRecord(name, now)); err != nil {
			t.Fatal(err)
		}
	}
	// A new store over the same directory stands in for a restarted server.
	loaded, issues := newSourceStore(dataDir).loadAll()
	if len(issues) != 0 {
		t.Fatalf("issues on clean reload: %+v", issues)
	}
	var names []string
	for _, r := range loaded {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "alpha,mid-1,zeta" {
		t.Fatalf("reload order: %v", names)
	}
	if loaded[0].Poll != 5*time.Minute || !loaded[0].CreatedAt.Equal(now) {
		t.Fatalf("reload lost fields: %+v", loaded[0])
	}
}

func TestSourceStoreMissingDirectoryIsEmptyNotAnError(t *testing.T) {
	loaded, issues := newSourceStore(t.TempDir()).loadAll()
	if len(loaded) != 0 || len(issues) != 0 {
		t.Fatalf("fresh data dir: records=%v issues=%v", loaded, issues)
	}
}

func TestSourceStoreSkipsBadFilesWithoutLeakingThem(t *testing.T) {
	dataDir := t.TempDir()
	dir := sourcesDir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	store := newSourceStore(dataDir)
	if err := store.save(sampleRecord("good", now)); err != nil {
		t.Fatal(err)
	}

	write := func(file, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("corrupt.json", `{"name": "corrupt", "url": "https://calendar.example/feed/`+sampleToken+`/cal.ics", `)
	write("mismatch.json", `{"name":"other","url":"https://calendar.example/x","poll":"5m0s"}`)
	write("UPPER.json", `{"name":"UPPER","url":"https://calendar.example/x","poll":"5m0s"}`)
	write("fastpoll.json", `{"name":"fastpoll","url":"https://calendar.example/x","poll":"30s"}`)
	write("plainhttp.json", `{"name":"plainhttp","url":"http://calendar.example/feed/`+sampleToken+`","poll":"5m0s"}`)
	write("notes.txt", "not a source")
	write("good.json.tmp-123", `{"name":"good"}`)
	if err := os.Mkdir(filepath.Join(dir, "nested.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	loaded, issues := newSourceStore(dataDir).loadAll()
	if len(loaded) != 1 || loaded[0].Name != "good" {
		t.Fatalf("loaded %+v, want only the good source", loaded)
	}
	if len(issues) != 5 {
		t.Fatalf("issues: got %d want 5 (corrupt, mismatch, UPPER, fastpoll, plainhttp): %+v", len(issues), issues)
	}
	for _, is := range issues {
		if strings.Contains(is.Reason, sampleToken) || strings.Contains(is.File, sampleToken) {
			t.Fatalf("issue leaks the token: %+v", is)
		}
	}
}

func TestSourceStoreRefusesUnsafeNames(t *testing.T) {
	store := newSourceStore(t.TempDir())
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for _, name := range []string{"", "../escape", "a/b", "UPPER", "-leading", strings.Repeat("a", 65)} {
		if err := store.save(sampleRecord(name, now)); err == nil {
			t.Errorf("save(%q) = nil error, want refusal", name)
		}
		if err := store.remove(name); err == nil {
			t.Errorf("remove(%q) = nil error, want refusal", name)
		}
	}
}

func TestSnapshotRemoveIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	store := newSnapshotStore(dataDir)
	if err := store.save(sampleSnapshot("personal", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := store.remove("personal"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, found, _ := store.load("personal"); found {
		t.Fatal("snapshot still present after remove")
	}
	if err := store.remove("personal"); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}
