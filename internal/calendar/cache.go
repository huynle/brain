package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// snapshot is the persisted state of one ics source: the last expanded
// window and the fetch bookkeeping around it. It is written to
// <dataDir>/calendars/<name>.json. It never holds the feed URL.
type snapshot struct {
	Name         string
	FirstSeen    time.Time
	LastFetch    time.Time
	LastSuccess  time.Time
	ETag         string
	LastModified string
	LastError    string
	Stale        bool
	// NotifiedEpisode is the stale-episode start already notified, so a
	// restart never raises the same stale notice twice.
	NotifiedEpisode time.Time
	// WindowEnd is where the occurrences' expansion window ends. It is set
	// by the last full (200) fetch.
	WindowEnd   time.Time
	Occurrences []Occurrence
}

// snapshotNamePattern is the calendar name shape config accepts. Names become
// file names, so anything outside it (separators, dots, spaces) is refused.
var snapshotNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// snapshotFile is the on-disk form of a snapshot. It is a separate type so
// the in-memory Occurrence can change without changing the file format.
type snapshotFile struct {
	Name            string           `json:"name"`
	FirstSeen       time.Time        `json:"first_seen"`
	LastFetch       time.Time        `json:"last_fetch"`
	LastSuccess     time.Time        `json:"last_success"`
	ETag            string           `json:"etag,omitempty"`
	LastModified    string           `json:"last_modified,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	Stale           bool             `json:"stale"`
	NotifiedEpisode time.Time        `json:"notified_episode"`
	WindowEnd       time.Time        `json:"window_end"`
	Occurrences     []occurrenceFile `json:"occurrences"`
}

type occurrenceFile struct {
	UID          string    `json:"uid"`
	Calendar     string    `json:"calendar"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day,omitempty"`
	Title        string    `json:"title,omitempty"`
	Description  string    `json:"description,omitempty"`
	Location     string    `json:"location,omitempty"`
	RecurrenceID time.Time `json:"recurrence_id"`
}

// snapshotStore reads and writes snapshots under one directory.
type snapshotStore struct {
	dir string
}

// newSnapshotStore returns a store rooted at <dataDir>/calendars. Nothing is
// created until the first save.
func newSnapshotStore(dataDir string) *snapshotStore {
	return &snapshotStore{dir: filepath.Join(dataDir, "calendars")}
}

// path returns the file for a calendar name, refusing names that could
// escape the store directory.
func (s *snapshotStore) path(name string) (string, error) {
	if !snapshotNamePattern.MatchString(name) {
		return "", fmt.Errorf("calendar snapshot name %q is not a safe file name", name)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

// save writes the snapshot atomically: a temp file in the same directory,
// synced, then renamed over the target. The directory is 0700 and the file
// 0600, because the occurrences are private calendar content.
func (s *snapshotStore) save(snap snapshot) error {
	target, err := s.path(snap.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create calendar snapshot directory: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("secure calendar snapshot directory: %w", err)
	}
	data, err := json.MarshalIndent(toSnapshotFile(snap), "", "  ")
	if err != nil {
		return fmt.Errorf("encode calendar snapshot %q: %w", snap.Name, err)
	}
	tmp, err := os.CreateTemp(s.dir, snap.Name+".json.tmp-*")
	if err != nil {
		return fmt.Errorf("create calendar snapshot temp file: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure calendar snapshot file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write calendar snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync calendar snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close calendar snapshot: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("commit calendar snapshot: %w", err)
	}
	committed = true
	return nil
}

// load reads the snapshot for name. A missing file is (zero, false, nil): the
// source has never been fetched. A corrupt or unreadable file is an error, so
// the caller can start that source fresh and say why.
func (s *snapshotStore) load(name string) (snapshot, bool, error) {
	target, err := s.path(name)
	if err != nil {
		return snapshot{}, false, err
	}
	data, err := os.ReadFile(target)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot{}, false, nil
	}
	if err != nil {
		return snapshot{}, false, fmt.Errorf("read calendar snapshot %q: %w", name, err)
	}
	var f snapshotFile
	if err := json.Unmarshal(data, &f); err != nil {
		return snapshot{}, false, fmt.Errorf("decode calendar snapshot %q: %w", name, err)
	}
	return fromSnapshotFile(f), true, nil
}

func toSnapshotFile(snap snapshot) snapshotFile {
	f := snapshotFile{
		Name:            snap.Name,
		FirstSeen:       snap.FirstSeen,
		LastFetch:       snap.LastFetch,
		LastSuccess:     snap.LastSuccess,
		ETag:            snap.ETag,
		LastModified:    snap.LastModified,
		LastError:       snap.LastError,
		Stale:           snap.Stale,
		NotifiedEpisode: snap.NotifiedEpisode,
		WindowEnd:       snap.WindowEnd,
		Occurrences:     make([]occurrenceFile, 0, len(snap.Occurrences)),
	}
	for _, o := range snap.Occurrences {
		f.Occurrences = append(f.Occurrences, occurrenceFile(o))
	}
	return f
}

func fromSnapshotFile(f snapshotFile) snapshot {
	snap := snapshot{
		Name:            f.Name,
		FirstSeen:       f.FirstSeen,
		LastFetch:       f.LastFetch,
		LastSuccess:     f.LastSuccess,
		ETag:            f.ETag,
		LastModified:    f.LastModified,
		LastError:       f.LastError,
		Stale:           f.Stale,
		NotifiedEpisode: f.NotifiedEpisode,
		WindowEnd:       f.WindowEnd,
		Occurrences:     make([]Occurrence, 0, len(f.Occurrences)),
	}
	for _, o := range f.Occurrences {
		snap.Occurrences = append(snap.Occurrences, Occurrence(o))
	}
	return snap
}
