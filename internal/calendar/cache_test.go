package calendar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleSnapshot(name string, now time.Time) snapshot {
	return snapshot{
		Name:         name,
		FirstSeen:    now.Add(-time.Hour),
		LastFetch:    now,
		LastSuccess:  now,
		ETag:         `"v1"`,
		LastModified: "Fri, 09 Oct 2026 10:00:00 GMT",
		LastError:    "",
		Stale:        false,
		Occurrences: []Occurrence{{
			UID:          "evt-1",
			Calendar:     name,
			Start:        now.Add(2 * time.Hour),
			End:          now.Add(3 * time.Hour),
			Title:        "standup",
			Location:     "room 4",
			RecurrenceID: now.Add(2 * time.Hour),
		}},
	}
}

func TestSnapshotRoundTripPreservesState(t *testing.T) {
	dataDir := t.TempDir()
	store := newSnapshotStore(dataDir)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	want := sampleSnapshot("team", now)

	if err := store.save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, found, err := store.load("team")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatal("load: snapshot not found after save")
	}
	if got.Name != want.Name || got.ETag != want.ETag || got.LastModified != want.LastModified {
		t.Fatalf("headers differ: got %+v want %+v", got, want)
	}
	if !got.LastSuccess.Equal(want.LastSuccess) || !got.LastFetch.Equal(want.LastFetch) || !got.FirstSeen.Equal(want.FirstSeen) {
		t.Fatalf("times differ: got %+v want %+v", got, want)
	}
	if len(got.Occurrences) != 1 {
		t.Fatalf("occurrences: got %d want 1", len(got.Occurrences))
	}
	o := got.Occurrences[0]
	if o.UID != "evt-1" || o.Calendar != "team" || o.Title != "standup" || o.Location != "room 4" {
		t.Fatalf("occurrence fields differ: %+v", o)
	}
	if !o.Start.Equal(want.Occurrences[0].Start) || !o.End.Equal(want.Occurrences[0].End) || !o.RecurrenceID.Equal(want.Occurrences[0].RecurrenceID) {
		t.Fatalf("occurrence times differ: %+v", o)
	}
}

func TestSnapshotFileAndDirectoryModes(t *testing.T) {
	dataDir := t.TempDir()
	store := newSnapshotStore(dataDir)
	if err := store.save(sampleSnapshot("team", time.Now().UTC())); err != nil {
		t.Fatalf("save: %v", err)
	}
	dir := filepath.Join(dataDir, "calendars")
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("directory mode: got %o want 700", mode)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, "team.json"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode: got %o want 600", mode)
	}
}

func TestSnapshotOverwriteIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dataDir := t.TempDir()
	store := newSnapshotStore(dataDir)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		s := sampleSnapshot("team", now.Add(time.Duration(i)*time.Minute))
		s.LastError = "poll " + string(rune('a'+i))
		if err := store.save(s); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	got, found, err := store.load("team")
	if err != nil || !found {
		t.Fatalf("load after overwrites: found=%v err=%v", found, err)
	}
	if got.LastError != "poll c" {
		t.Fatalf("last write did not win: got %q", got.LastError)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "calendars"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "team.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only team.json, found %v", names)
	}
}

func TestSnapshotMissingIsNotAnError(t *testing.T) {
	store := newSnapshotStore(t.TempDir())
	_, found, err := store.load("team")
	if err != nil {
		t.Fatalf("missing snapshot returned error: %v", err)
	}
	if found {
		t.Fatal("missing snapshot reported as found")
	}
}

func TestSnapshotCorruptFileIsAnError(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "calendars")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, found, err := newSnapshotStore(dataDir).load("team")
	if err == nil {
		t.Fatal("corrupt snapshot loaded without error")
	}
	if found {
		t.Fatal("corrupt snapshot reported as found")
	}
}

func TestSnapshotRejectsUnsafeNames(t *testing.T) {
	store := newSnapshotStore(t.TempDir())
	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "has space"} {
		if err := store.save(sampleSnapshot(name, time.Now().UTC())); err == nil {
			t.Errorf("save accepted unsafe name %q", name)
		}
		if _, _, err := store.load(name); err == nil {
			t.Errorf("load accepted unsafe name %q", name)
		}
	}
}
