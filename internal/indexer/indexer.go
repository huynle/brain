package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/pkg/markdown"
)

// EmbeddingClient defines the interface for generating embeddings.
// This is defined here to avoid import cycles with internal/service.
type EmbeddingClient interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// Indexer synchronizes markdown files on disk with the SQLite database.
type Indexer struct {
	brainDir string
	storage  *storage.TenantStore
	policy   *tenantfs.Root

	// mu serializes every index write: each IndexFile and RemoveFile call,
	// and each per-path step of a scan (parse + compare + write, or delete).
	// A writer's parse and row write are therefore atomic with respect to the
	// scan's, so neither side acts on a check the other just invalidated
	// (lost rows, stale overwrites, "duplicate path" inserts).
	mu sync.Mutex
	// written is non-nil while a scan runs. IndexFile and RemoveFile record
	// the paths they changed in it, under mu. The scan never writes or deletes
	// a recorded path: that writer's result is newer than the scan's
	// snapshots and wins.
	written map[string]struct{}
	// scanMu serializes whole scans (IndexChanged, RebuildAll) so only one
	// scan owns written at a time.
	scanMu sync.Mutex

	// scanHook, when non-nil, is invoked at fixed points inside IndexChanged
	// so tests can interleave writers deterministically. Production never
	// sets it.
	scanHook func(stage scanStage, path string)
}

// scanStage names a point inside IndexChanged where scanHook is invoked.
type scanStage int

const (
	// scanStageDiscovered fires once, after the disk walk and before the
	// scan reads its snapshot of indexed rows.
	scanStageDiscovered scanStage = iota
	// scanStageParsed fires after the scan parsed one discovered file and
	// before it writes that file's row, inside that path's critical section.
	scanStageParsed
	// scanStageStep fires at the start of each discovered file's step,
	// inside that path's critical section, before anything is checked.
	scanStageStep
	// scanStageWalk fires for each in-scope entry the discovery walk visits,
	// before the entry is admitted.
	scanStageWalk
)

func (idx *Indexer) hook(stage scanStage, path string) {
	if idx.scanHook != nil {
		idx.scanHook(stage, path)
	}
}

// NewIndexer creates a new Indexer for the given brain directory and storage layer.
func NewIndexer(brainDir string, store *storage.TenantStore, policy ...*tenantfs.Root) *Indexer {
	idx := &Indexer{
		brainDir: brainDir,
		storage:  store,
	}
	if len(policy) > 0 {
		idx.policy = policy[0]
	}
	return idx
}

// FilesystemPolicy is the immutable tenant binding shared with service writers
// and watchers. Only trusted composition supplies it; it never provisions roots.
func (idx *Indexer) FilesystemPolicy() *tenantfs.Root { return idx.policy }

func (idx *Indexer) admit(name string, missing bool) error {
	if idx.policy == nil {
		return nil
	}
	if missing {
		_, err := idx.policy.ResolveForWrite(context.Background(), name)
		return err
	}
	return idx.policy.AdmitTraversal(context.Background(), name)
}

func (idx *Indexer) markdownFiles() ([]string, error) {
	return walkMarkdownFiles(idx.brainDir, idx.policy, func(rel string) { idx.hook(scanStageWalk, rel) })
}

// RebuildAll performs a full rebuild: deletes this tenant's notes and re-indexes
// every discovered .md file under projects/ and global/.
//
// It holds the write lock for its whole run: it deletes every row before
// reinserting from its own disk snapshot, so a concurrent writer's row would
// otherwise be lost. It has no production caller; writers simply wait.
func (idx *Indexer) RebuildAll() (*IndexResult, error) {
	idx.scanMu.Lock()
	defer idx.scanMu.Unlock()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	start := time.Now()
	ctx := context.Background()
	var indexErrors []IndexError

	// 1. Discover files on disk
	files, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}

	// 2. Parse all files, collecting results and errors
	var parsed []*markdown.ParsedFile
	for _, file := range files {
		if err := idx.admit(file, false); err != nil {
			return nil, err
		}
		pf, err := markdown.ParseFile(file, idx.brainDir)
		if err != nil {
			indexErrors = append(indexErrors, IndexError{
				Path:  file,
				Error: err.Error(),
			})
			continue
		}
		parsed = append(parsed, pf)
	}

	// 3. Clear this tenant's notes (CASCADE cleans dependent content).
	existingCount, err := idx.storage.DeleteAllNotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete all notes: %w", err)
	}

	// 4. Insert each parsed file
	for _, pf := range parsed {
		row := toNoteRow(pf)
		inserted, err := idx.storage.InsertNote(ctx, &row)
		if err != nil {
			return nil, fmt.Errorf("insert note %q: %w", pf.Path, err)
		}
		_ = inserted

		if len(pf.Tags) > 0 {
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return nil, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
		}

		if len(pf.Links) > 0 {
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return nil, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
		}
	}

	return &IndexResult{
		Added:    len(parsed),
		Updated:  0,
		Deleted:  int(existingCount),
		Skipped:  0,
		Errors:   indexErrors,
		Duration: time.Since(start),
	}, nil
}

// IndexChanged performs an incremental index: compares files on disk with DB,
// only processing changes.
//
// It runs concurrently with writers (the boot scan runs in the background
// while the server serves saves and ensures its built-in automations). Its
// disk walk and row snapshot are taken at different times, so for any path a
// writer indexes or removes after the scan started, both snapshots may be
// stale. The scan therefore records such paths (see Indexer.written) and
// leaves them to the writer: it never inserts, updates or deletes their rows.
func (idx *Indexer) IndexChanged() (*IndexResult, error) {
	idx.scanMu.Lock()
	defer idx.scanMu.Unlock()
	start := time.Now()
	ctx := context.Background()
	var indexErrors []IndexError
	var added, updated, deleted, skipped int

	// 0. Start recording writer-owned paths BEFORE either snapshot, so every
	// write the snapshots might miss is recorded.
	idx.mu.Lock()
	idx.written = make(map[string]struct{})
	idx.mu.Unlock()
	defer func() {
		idx.mu.Lock()
		idx.written = nil
		idx.mu.Unlock()
	}()

	// 1. Discover files on disk
	diskFiles, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}
	idx.hook(scanStageDiscovered, "")

	// 2. Get all existing notes from DB (path + checksum)
	states, err := idx.storage.ListIndexedNoteStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("query existing notes: %w", err)
	}
	dbMap := make(map[string]*string) // path → checksum (nullable)
	for _, state := range states {
		dbMap[state.Path] = state.Checksum
	}

	// Per-path critical sections. They are closures, not methods, so the
	// storage call sites stay inside IndexChanged as recorded by the storage
	// ownership ratchet (internal/storage/testdata/storage_unscoped_sites.golden)
	// and the reviewed provenance baselines.
	//
	// deleteRow is the scan's only row deletion. Callers hold idx.mu and have
	// checked that no writer owns path.
	deleteRow := func(path string) error {
		if _, err := idx.storage.DeleteNote(ctx, path); err != nil {
			return fmt.Errorf("delete note %q: %w", path, err)
		}
		return nil
	}
	// vanished handles a discovered file that is gone by the time its step
	// runs (an out-of-band rm, or a writer's delete/move whose RemoveFile is
	// still waiting for idx.mu). The file is treated as absent: its snapshot
	// row, if any, is deleted, and the scan continues.
	vanished := func(file string) (scanOutcome, error) {
		if _, inDB := dbMap[file]; !inDB {
			return scanOutcome{kind: scanGone}, nil
		}
		if err := deleteRow(file); err != nil {
			return scanOutcome{}, err
		}
		return scanOutcome{kind: scanDeleted}, nil
	}
	// scanFile reconciles one discovered file against the row snapshot. The
	// snapshot is valid for this path unless a writer touched it since the
	// scan began, in which case the writer's row stands and it is skipped.
	scanFile := func(file string) (scanOutcome, error) {
		idx.mu.Lock()
		defer idx.mu.Unlock()
		idx.hook(scanStageStep, file)
		if idx.writtenDuringScan(file) {
			return scanOutcome{kind: scanSkipped}, nil
		}
		// Every failure other than a confirmed vanish keeps its previous
		// handling: admission failures abort the scan, read/parse failures
		// are reported in IndexResult.Errors.
		if err := idx.admit(file, false); err != nil {
			if idx.isVanished(file, err) {
				return vanished(file)
			}
			return scanOutcome{}, err
		}
		pf, err := markdown.ParseFile(file, idx.brainDir)
		if err != nil {
			if idx.isVanished(file, err) {
				return vanished(file)
			}
			return scanOutcome{kind: scanParseError, parseErr: IndexError{Path: file, Error: err.Error()}}, nil
		}
		idx.hook(scanStageParsed, file)

		existingChecksum, inDB := dbMap[file]
		switch {
		case !inDB:
			// New file — not in DB
			row := toNoteRow(pf)
			if _, err := idx.storage.InsertNote(ctx, &row); err != nil {
				return scanOutcome{}, fmt.Errorf("insert note %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return scanOutcome{}, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return scanOutcome{}, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
			return scanOutcome{kind: scanAdded}, nil
		case existingChecksum == nil || *existingChecksum != pf.Checksum:
			// Modified file — checksum differs
			if _, err := idx.storage.UpdateNote(ctx, pf.Path, noteUpdateMap(pf)); err != nil {
				return scanOutcome{}, fmt.Errorf("update note %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return scanOutcome{}, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return scanOutcome{}, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
			return scanOutcome{kind: scanUpdated}, nil
		default:
			// Unchanged — skip
			return scanOutcome{kind: scanSkipped}, nil
		}
	}
	// scanDelete removes a snapshot row the disk walk did not find, unless a
	// writer indexed or removed that path since the scan began (the walk may
	// have passed its directory before the writer's file landed).
	scanDelete := func(dbPath string) (bool, error) {
		idx.mu.Lock()
		defer idx.mu.Unlock()
		if idx.writtenDuringScan(dbPath) {
			return false, nil
		}
		if err := deleteRow(dbPath); err != nil {
			return false, err
		}
		return true, nil
	}

	// 3. Process each file on disk, one critical section per path.
	for _, file := range diskFiles {
		outcome, err := scanFile(file)
		if err != nil {
			return nil, err
		}
		switch outcome.kind {
		case scanAdded:
			added++
		case scanUpdated:
			updated++
		case scanSkipped:
			skipped++
		case scanDeleted:
			deleted++
		case scanParseError:
			indexErrors = append(indexErrors, outcome.parseErr)
		}
	}

	// 4. Delete DB entries absent from scoped discovery, including formerly
	// indexed out-of-scope files that still exist on disk. Disk files are untouched.
	for dbPath := range dbMap {
		if diskSet[dbPath] {
			continue
		}
		removed, err := scanDelete(dbPath)
		if err != nil {
			return nil, err
		}
		if removed {
			deleted++
		}
	}

	return &IndexResult{
		Added:    added,
		Updated:  updated,
		Deleted:  deleted,
		Skipped:  skipped,
		Errors:   indexErrors,
		Duration: time.Since(start),
	}, nil
}

type scanOutcomeKind int

const (
	scanSkipped scanOutcomeKind = iota
	scanAdded
	scanUpdated
	scanParseError
	// scanDeleted: the file vanished before its step; its row was deleted.
	scanDeleted
	// scanGone: the file vanished before its step and had no row.
	scanGone
)

type scanOutcome struct {
	kind     scanOutcomeKind
	parseErr IndexError
}

// isVanished reports whether err from admitting or reading a discovered file
// means the file itself is gone: the error is ErrNotExist AND nothing exists
// at the path any more. A dangling symlink or a missing symlink target is not a
// vanish (Lstat still finds the link) and keeps its fatal handling, matching
// discovery.
func (idx *Indexer) isVanished(file string, err error) bool {
	return vanishedPath(idx.brainDir, file, err)
}

// vanishedPath is isVanished for a slash-separated path relative to brainDir.
// The root itself never counts as vanished.
func vanishedPath(brainDir, rel string, err error) bool {
	if rel == "." || !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	_, lerr := os.Lstat(filepath.Join(brainDir, filepath.FromSlash(rel)))
	return errors.Is(lerr, fs.ErrNotExist)
}

// writtenDuringScan reports whether a writer changed path since the current
// scan began. Callers hold idx.mu.
func (idx *Indexer) writtenDuringScan(path string) bool {
	_, ok := idx.written[path]
	return ok
}

// recordWrite marks path as writer-owned for the current scan, if any.
// Callers hold idx.mu.
func (idx *Indexer) recordWrite(path string) {
	if idx.written != nil {
		idx.written[path] = struct{}{}
	}
}

// IndexFile indexes a single file by relative path (upsert). It is serialized
// with other writers and with each per-path step of a concurrent scan, and
// claims the path for itself for the rest of that scan.
func (idx *Indexer) IndexFile(relativePath string) error {
	ctx := context.Background()
	if idx.policy != nil {
		if _, err := idx.policy.Resolve(ctx, relativePath); err != nil {
			return err
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	pf, err := markdown.ParseFile(relativePath, idx.brainDir)
	if err != nil {
		return fmt.Errorf("parse file %q: %w", relativePath, err)
	}
	idx.recordWrite(relativePath)

	existing, err := idx.storage.GetNoteByPath(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("check existing note %q: %w", relativePath, err)
	}

	if existing != nil {
		// Update
		updates := noteUpdateMap(pf)
		if _, err := idx.storage.UpdateNote(ctx, relativePath, updates); err != nil {
			return fmt.Errorf("update note %q: %w", relativePath, err)
		}
	} else {
		// Insert
		row := toNoteRow(pf)
		if _, err := idx.storage.InsertNote(ctx, &row); err != nil {
			return fmt.Errorf("insert note %q: %w", relativePath, err)
		}
	}

	if err := idx.storage.SetTags(ctx, relativePath, pf.Tags); err != nil {
		return fmt.Errorf("set tags for %q: %w", relativePath, err)
	}
	if err := idx.storage.SetLinks(ctx, relativePath, toLinkInputs(pf.Links)); err != nil {
		return fmt.Errorf("set links for %q: %w", relativePath, err)
	}

	return nil
}

// RemoveFile removes a single file from the index. Like IndexFile it is
// serialized with scans and claims the path for the rest of a running scan,
// so the scan cannot resurrect the row from an older parse.
func (idx *Indexer) RemoveFile(relativePath string) error {
	ctx := context.Background()
	if err := idx.admit(relativePath, true); err != nil {
		return err
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.recordWrite(relativePath)
	_, err := idx.storage.DeleteNote(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("delete note %q: %w", relativePath, err)
	}
	return nil
}

// GetHealth returns health statistics about the index.
func (idx *Indexer) GetHealth() (*IndexHealth, error) {
	// Count disk files
	diskFiles, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}

	// Count indexed and stale entries from one tenant-scoped snapshot.
	states, err := idx.storage.ListIndexedNoteStates(context.Background())
	if err != nil {
		return nil, fmt.Errorf("query indexed notes: %w", err)
	}

	var staleCount int
	for _, state := range states {
		if !diskSet[state.Path] {
			staleCount++
		}
	}

	return &IndexHealth{
		TotalFiles:   len(diskFiles),
		TotalIndexed: len(states),
		StaleCount:   staleCount,
	}, nil
}

// IndexEmbeddings generates and stores embeddings for notes that need them.
// It uses staleness checks to avoid re-embedding unchanged notes.
// Embedding generation is best-effort: errors are logged but do not break the process.
func (idx *Indexer) IndexEmbeddings(ctx context.Context, embeddingClient EmbeddingClient) (*EmbeddingIndexResult, error) {
	return idx.IndexEmbeddingsWithOptions(ctx, embeddingClient, EmbeddingIndexOptions{})
}

// EmbeddingIndexOptions filters and controls embedding generation.
type EmbeddingIndexOptions struct {
	Project string
	Path    string
	Force   bool
}

// ListEmbeddingBackfillCandidates returns notes matching an embedding backfill request.
func (idx *Indexer) ListEmbeddingBackfillCandidates(ctx context.Context, opts EmbeddingIndexOptions) ([]EmbeddingBackfillCandidate, error) {
	notes, err := idx.storage.ListEmbeddingNotes(ctx, opts.Project, opts.Path, opts.Force)
	if err != nil {
		return nil, err
	}
	var candidates []EmbeddingBackfillCandidate
	for _, n := range notes {
		candidates = append(candidates, EmbeddingBackfillCandidate{ID: n.ID, Path: n.Path, Title: n.Title, Project: n.ProjectID, Type: n.Type})
	}
	return candidates, nil
}

// IndexEmbeddingsWithOptions generates and stores embeddings for matching notes.
func (idx *Indexer) IndexEmbeddingsWithOptions(ctx context.Context, embeddingClient EmbeddingClient, opts EmbeddingIndexOptions) (*EmbeddingIndexResult, error) {
	if embeddingClient == nil {
		return nil, fmt.Errorf("embedding client is nil")
	}

	start := time.Now()
	var processed, skipped, failed int

	ownedNotes, err := idx.storage.ListEmbeddingNotes(ctx, opts.Project, opts.Path, opts.Force)
	if err != nil {
		return nil, fmt.Errorf("query stale notes: %w", err)
	}

	// Process each note
	for _, note := range ownedNotes {
		if opts.Force {
			if err := idx.storage.DeleteNoteEmbeddings(ctx, note.ID); err != nil {
				slog.Warn("failed to delete existing embeddings for note", "note_id", note.ID, "error", err)
				failed++
				continue
			}
		}
		embeddingSource, err := idx.embeddingSourceForNote(ctx, note.ID)
		if err != nil {
			slog.Warn("failed to build embedding source for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		// Skip notes whose combined embedding source is empty.
		if strings.TrimSpace(embeddingSource) == "" {
			skipped++
			continue
		}

		// Generate chunks
		chunks := markdown.ChunkNote(note.ID, embeddingSource)
		if len(chunks) == 0 {
			skipped++
			continue
		}

		// Extract chunk texts for embedding
		chunkTexts := make([]string, len(chunks))
		for i, chunk := range chunks {
			chunkTexts[i] = chunk.Text
		}

		// Generate embeddings (best-effort)
		embeddings, err := embeddingClient.Embed(ctx, chunkTexts)
		if err != nil {
			slog.Warn("failed to generate embeddings for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		if len(embeddings) != len(chunks) {
			slog.Warn("embedding count mismatch",
				"note_id", note.ID,
				"expected", len(chunks),
				"got", len(embeddings))
			failed++
			continue
		}

		// Build EmbeddingRecord slice
		records := make([]storage.EmbeddingRecord, len(chunks))
		for i, chunk := range chunks {
			records[i] = storage.EmbeddingRecord{
				NoteID:     note.ID,
				ChunkIndex: chunk.ChunkIndex,
				Vector:     embeddings[i],
				ProjectID:  note.ProjectID,
				Type:       note.Type,
				Status:     note.Status,
				FeatureID:  note.FeatureID,
				Priority:   note.Priority,
			}
		}

		// Upsert embeddings (best-effort)
		if err := idx.storage.UpsertNoteEmbeddings(ctx, records); err != nil {
			slog.Warn("failed to upsert embeddings for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		processed++
	}

	return &EmbeddingIndexResult{
		Processed: processed,
		Skipped:   skipped,
		Failed:    failed,
		Duration:  time.Since(start),
	}, nil
}

func (idx *Indexer) embeddingSourceForNote(ctx context.Context, noteID int64) (string, error) {
	return idx.storage.EmbeddingSource(ctx, noteID)
}

// GetEmbeddingHealth returns statistics about the embedding index.
func (idx *Indexer) GetEmbeddingHealth() (*EmbeddingHealth, error) {
	totalNotes, notesWithEmbeddings, staleEmbeddings, err := idx.storage.EmbeddingHealthCounts(context.Background())
	if err != nil {
		return nil, err
	}

	// Count notes without embeddings
	notesWithoutEmbeddings := totalNotes - notesWithEmbeddings

	return &EmbeddingHealth{
		TotalNotes:             totalNotes,
		NotesWithEmbeddings:    notesWithEmbeddings,
		NotesWithoutEmbeddings: notesWithoutEmbeddings,
		StaleEmbeddings:        staleEmbeddings,
	}, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// toNoteRow maps a ParsedFile to a NoteRow for DB insertion.
func toNoteRow(pf *markdown.ParsedFile) storage.NoteRow {
	metadataJSON := "{}"
	if pf.Metadata != nil {
		if b, err := json.Marshal(pf.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	return storage.NoteRow{
		Path:       pf.Path,
		ShortID:    pf.ShortID,
		Title:      pf.Title,
		Lead:       strPtr(pf.Lead),
		Body:       strPtr(pf.Body),
		RawContent: strPtr(pf.RawContent),
		WordCount:  pf.WordCount,
		Checksum:   strPtr(pf.Checksum),
		Metadata:   metadataJSON,
		Type:       pf.Type,
		Status:     pf.Status,
		Priority:   pf.Priority,
		ProjectID:  pf.ProjectID,
		FeatureID:  pf.FeatureID,
		Created:    strPtr(pf.Created),
		Modified:   strPtr(pf.Modified),
	}
}

// noteUpdateMap builds the update map for storage.UpdateNote from a ParsedFile.
func noteUpdateMap(pf *markdown.ParsedFile) map[string]interface{} {
	metadataJSON := "{}"
	if pf.Metadata != nil {
		if b, err := json.Marshal(pf.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	return map[string]interface{}{
		"title":       pf.Title,
		"lead":        strPtr(pf.Lead),
		"body":        strPtr(pf.Body),
		"raw_content": strPtr(pf.RawContent),
		"word_count":  pf.WordCount,
		"checksum":    strPtr(pf.Checksum),
		"metadata":    metadataJSON,
		"type":        pf.Type,
		"status":      pf.Status,
		"priority":    pf.Priority,
		"project_id":  pf.ProjectID,
		"feature_id":  pf.FeatureID,
		"created":     strPtr(pf.Created),
		"modified":    strPtr(pf.Modified),
	}
}

// toLinkInputs maps ExtractedLink slice to LinkInput slice.
func toLinkInputs(links []markdown.ExtractedLink) []storage.LinkInput {
	inputs := make([]storage.LinkInput, len(links))
	for i, link := range links {
		inputs[i] = storage.LinkInput{
			TargetPath: link.Href,
			Title:      link.Title,
			Href:       link.Href,
			Type:       link.Type,
			Snippet:    link.Snippet,
		}
	}
	return inputs
}

// inContentScope checks the exact first component of a slash-separated relative
// path. Callers handle the traversal root separately. This is a discovery policy,
// not a replacement for the parser's path containment checks.
func inContentScope(relativePath string) bool {
	first, _, _ := strings.Cut(relativePath, "/")
	return first == "projects" || first == "global"
}

// globMarkdownFiles returns relative .md paths under projects/ and global/ only.
// WalkDir prunes other top-level directories before reading their contents and
// does not recurse through directory symlinks.
func globMarkdownFiles(brainDir string, policies ...*tenantfs.Root) ([]string, error) {
	var policy *tenantfs.Root
	if len(policies) > 0 {
		policy = policies[0]
	}
	return walkMarkdownFiles(brainDir, policy, nil)
}

// walkMarkdownFiles implements globMarkdownFiles. visit, when non-nil, is
// called for each in-scope entry before it is admitted.
func walkMarkdownFiles(brainDir string, policy *tenantfs.Root, visit func(rel string)) ([]string, error) {
	var files []string
	// An entry can vanish between its parent's directory read and its own
	// visit (writers and out-of-band edits run during the walk). A vanished
	// entry is simply not discovered: skip it, or its subtree. Every other
	// error, including a dangling symlink, stays fatal.
	skipVanished := func(relPath string, d fs.DirEntry, err error) error {
		if !vanishedPath(brainDir, relPath, err) {
			return err
		}
		if d != nil && d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	err := filepath.WalkDir(brainDir, func(path string, d fs.DirEntry, walkErr error) error {
		// Get relative path
		relPath, err := filepath.Rel(brainDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if walkErr != nil {
			return skipVanished(relPath, d, walkErr)
		}
		// Admit the root itself, but prune unrelated siblings before resolving
		// tenant policy (an excluded dangling symlink is not content).
		if relPath != "." && !inContentScope(relPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if visit != nil && relPath != "." {
			visit(relPath)
		}
		if policy != nil {
			if err := policy.AdmitTraversal(context.Background(), relPath); err != nil {
				if relPath == "." || !errors.Is(err, tenantfs.ErrDenied) {
					return skipVanished(relPath, d, err)
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if relPath == "." {
			return nil
		}
		// Only .md files
		if !d.IsDir() && strings.HasSuffix(relPath, ".md") {
			// Normalize to forward slashes for consistency
			files = append(files, filepath.ToSlash(relPath))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// strPtr returns a pointer to s, or nil if s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
