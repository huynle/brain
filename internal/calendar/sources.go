package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// runtimeNamePattern is the shape of a runtime source name: the config rule
// with a 64-character limit. Names become file names, so separators and dots
// are refused.
var runtimeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// maxAddressLength bounds a source address.
const maxAddressLength = 2048

// sourceRecord is the persisted definition of one runtime source. It holds
// the address, which is a secret: the file is 0600 in a 0700 directory, and
// the record is never returned by any view.
type sourceRecord struct {
	Name      string
	URL       string
	Poll      time.Duration
	CreatedAt time.Time
	UpdatedAt time.Time
}

// sourceFile is the on-disk form of a sourceRecord. The poll is stored as a
// Go duration string so the file reads naturally.
type sourceFile struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Poll      string    `json:"poll"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// sourceIssue names one source file the store skipped, and why. Neither field
// ever carries the file's contents, because the contents hold the address.
type sourceIssue struct {
	File   string
	Reason string
}

// sourceStore reads and writes runtime source records under
// <dataDir>/calendars/sources.
type sourceStore struct {
	dir string
}

func newSourceStore(dataDir string) *sourceStore {
	return &sourceStore{dir: filepath.Join(dataDir, "calendars", "sources")}
}

// validRuntimeName reports whether name is a legal runtime source name.
func validRuntimeName(name string) bool {
	return runtimeNamePattern.MatchString(name)
}

// path returns the file for a source name, refusing names that could escape
// the store directory.
func (s *sourceStore) path(name string) (string, error) {
	if !validRuntimeName(name) {
		return "", errors.New("calendar source name is not a safe file name")
	}
	return filepath.Join(s.dir, name+".json"), nil
}

// save writes the record atomically: a temp file in the same directory,
// synced, then renamed over the target. The directory is 0700 and the file
// 0600, because the file holds the address.
func (s *sourceStore) save(rec sourceRecord) error {
	target, err := s.path(rec.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create calendar source directory: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("secure calendar source directory: %w", err)
	}
	data, err := json.MarshalIndent(toSourceFile(rec), "", "  ")
	if err != nil {
		return fmt.Errorf("encode calendar source %q: %w", rec.Name, err)
	}
	tmp, err := os.CreateTemp(s.dir, rec.Name+".json.tmp-*")
	if err != nil {
		return fmt.Errorf("create calendar source temp file: %w", err)
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
		return fmt.Errorf("secure calendar source file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write calendar source: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync calendar source: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close calendar source: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("commit calendar source: %w", err)
	}
	committed = true
	// Syncing the directory makes the rename itself durable. Best effort:
	// some filesystems refuse to sync a directory.
	if d, err := os.Open(s.dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// remove deletes a source's file. A missing file is not an error, so removal
// can be retried.
func (s *sourceStore) remove(name string) error {
	target, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove calendar source file: %w", err)
	}
	return nil
}

// loadAll reads every source file. A missing directory is an empty store. A
// file that is corrupt, unreadable, not regular, misnamed, or holds an invalid
// poll or address is skipped and reported as an issue, never as an error, so
// one bad file cannot stop the server. Temp files and directories are ignored.
// Records come back sorted by name.
func (s *sourceStore) loadAll() ([]sourceRecord, []sourceIssue) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []sourceIssue{{File: "sources", Reason: "directory is unreadable"}}
	}
	var records []sourceRecord
	var issues []sourceIssue
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || !strings.HasSuffix(file, ".json") {
			continue
		}
		if !e.Type().IsRegular() {
			issues = append(issues, sourceIssue{File: file, Reason: "not a regular file"})
			continue
		}
		name := strings.TrimSuffix(file, ".json")
		if !validRuntimeName(name) {
			issues = append(issues, sourceIssue{File: file, Reason: "file name is not a valid source name"})
			continue
		}
		rec, reason := s.readOne(file, name)
		if reason != "" {
			issues = append(issues, sourceIssue{File: file, Reason: reason})
			continue
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, issues
}

// readOne loads and checks one source file. A non-empty reason means the file
// was skipped.
func (s *sourceStore) readOne(file, name string) (sourceRecord, string) {
	data, err := os.ReadFile(filepath.Join(s.dir, file))
	if err != nil {
		return sourceRecord{}, "file is unreadable"
	}
	var f sourceFile
	if err := json.Unmarshal(data, &f); err != nil {
		// The decoder's message can quote file content, so it is dropped.
		return sourceRecord{}, "file is corrupt"
	}
	if f.Name != name {
		return sourceRecord{}, "name does not match the file name"
	}
	poll, err := time.ParseDuration(f.Poll)
	if err != nil {
		return sourceRecord{}, "poll is not a duration"
	}
	if poll < minPollInterval {
		return sourceRecord{}, "poll is below one minute"
	}
	if err := validateAddress(f.URL); err != nil {
		return sourceRecord{}, err.Error()
	}
	return sourceRecord{
		Name:      f.Name,
		URL:       f.URL,
		Poll:      poll,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}, ""
}

// validateAddress checks the shape of a runtime source address: https, a
// host, no user information, and at most 2048 characters. Its errors are
// fixed text. url.Parse errors quote the input, so they are never returned.
func validateAddress(raw string) error {
	if raw == "" {
		return errors.New("address is empty")
	}
	if len(raw) > maxAddressLength {
		return errors.New("address is longer than 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("address is not a valid URL")
	}
	if u.Scheme != "https" {
		return errors.New("address must use https")
	}
	if u.Hostname() == "" {
		return errors.New("address has no host")
	}
	if u.User != nil {
		return errors.New("address must not contain user information")
	}
	return nil
}

func toSourceFile(rec sourceRecord) sourceFile {
	return sourceFile{
		Name:      rec.Name,
		URL:       rec.URL,
		Poll:      rec.Poll.String(),
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}
}
