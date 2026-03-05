package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
	"github.com/huynle/brain-api/pkg/markdown"
)

// Compile-time check that BrainServiceImpl implements api.BrainService.
var _ api.BrainService = (*BrainServiceImpl)(nil)

// BrainServiceImpl implements api.BrainService using filesystem + SQLite storage.
type BrainServiceImpl struct {
	config  *config.Config
	storage *storage.StorageLayer
	indexer *indexer.Indexer
}

// NewBrainService creates a new BrainServiceImpl.
func NewBrainService(cfg *config.Config, store *storage.StorageLayer, idx *indexer.Indexer) *BrainServiceImpl {
	return &BrainServiceImpl{
		config:  cfg,
		storage: store,
		indexer: idx,
	}
}

// =============================================================================
// Save
// =============================================================================

// Save creates a new brain entry on disk and indexes it.
func (s *BrainServiceImpl) Save(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
	// Validate required fields
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Sanitize inputs
	title := frontmatter.SanitizeTitle(req.Title)

	var sanitizedTags []string
	for _, tag := range req.Tags {
		if st, ok := frontmatter.SanitizeTag(tag); ok {
			sanitizedTags = append(sanitizedTags, st)
		}
	}

	var sanitizedDeps []string
	for _, dep := range req.DependsOn {
		if sd := frontmatter.SanitizeDependsOnEntry(dep); sd != "" {
			sanitizedDeps = append(sanitizedDeps, sd)
		}
	}

	// Generate short ID
	shortID := markdown.GenerateShortID()

	// Determine status
	status := req.Status
	if status == "" {
		status = "active"
	}

	// Determine project
	isGlobal := req.Global != nil && *req.Global
	project := req.Project
	if project == "" {
		project = "default"
	}

	// Compute relative path
	var relPath string
	if isGlobal {
		relPath = filepath.Join("global", req.Type, shortID+".md")
	} else {
		relPath = filepath.Join("projects", project, req.Type, shortID+".md")
	}
	// Normalize to forward slashes for consistency
	relPath = filepath.ToSlash(relPath)

	// Generate created timestamp
	now := types.TimeNowUTC().Format(time.RFC3339)

	// Build frontmatter options
	opts := &frontmatter.GenerateOptions{
		Title:               title,
		Type:                req.Type,
		Status:              status,
		Tags:                sanitizedTags,
		Priority:            req.Priority,
		Created:             now,
		DependsOn:           sanitizedDeps,
		FeatureID:           req.FeatureID,
		FeaturePriority:     req.FeaturePriority,
		FeatureDependsOn:    req.FeatureDependsOn,
		Workdir:             frontmatter.SanitizeSimpleValue(req.Workdir),
		GitRemote:           frontmatter.SanitizeSimpleValue(req.GitRemote),
		GitBranch:           frontmatter.SanitizeSimpleValue(req.GitBranch),
		MergeTargetBranch:   req.MergeTargetBranch,
		MergePolicy:         req.MergePolicy,
		MergeStrategy:       req.MergeStrategy,
		RemoteBranchPolicy:  req.RemoteBranchPolicy,
		OpenPRBeforeMerge:   req.OpenPRBeforeMerge,
		ExecutionMode:       req.ExecutionMode,
		CompleteOnIdle:      req.CompleteOnIdle,
		TargetWorkdir:       frontmatter.SanitizeSimpleValue(req.TargetWorkdir),
		UserOriginalRequest: req.UserOriginalRequest,
		DirectPrompt:        req.DirectPrompt,
		Agent:               req.Agent,
		Model:               req.Model,
		Generated:           req.Generated,
		GeneratedKind:       req.GeneratedKind,
		GeneratedKey:        req.GeneratedKey,
		GeneratedBy:         req.GeneratedBy,
		Schedule:            req.Schedule,
		ScheduleEnabled:     req.ScheduleEnabled,
		NextRun:             req.NextRun,
		MaxRuns:             req.MaxRuns,
		StartsAt:            req.StartsAt,
		ExpiresAt:           req.ExpiresAt,
	}

	if !isGlobal {
		opts.ProjectID = project
	}

	// Generate frontmatter YAML
	fmYAML := frontmatter.Generate(opts)

	// Build full file content
	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString(fmYAML)
	content.WriteString("---\n")
	if req.Content != "" {
		content.WriteString("\n")
		content.WriteString(req.Content)
		content.WriteString("\n")
	}

	// Write file to disk
	absPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(relPath))
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %q: %w", dir, err)
	}
	if err := os.WriteFile(absPath, []byte(content.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write file %q: %w", absPath, err)
	}

	// Index the file
	if err := s.indexer.IndexFile(relPath); err != nil {
		return nil, fmt.Errorf("index file %q: %w", relPath, err)
	}

	// Generate markdown link
	link := markdown.GenerateMarkdownLink(shortID, title)

	return &types.CreateEntryResponse{
		ID:     shortID,
		Path:   relPath,
		Title:  title,
		Type:   req.Type,
		Status: status,
		Link:   link,
	}, nil
}

// =============================================================================
// Recall
// =============================================================================

// Recall retrieves a brain entry by path, short ID, or title.
func (s *BrainServiceImpl) Recall(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
	if pathOrID == "" {
		return nil, fmt.Errorf("path or ID is required")
	}

	row, err := s.resolveEntry(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, api.ErrNotFound
	}

	entry := NoteRowToBrainEntry(row)

	// Record access
	_ = s.storage.RecordAccess(ctx, row.Path)

	// Populate access count from meta
	meta, err := s.storage.GetAccessStats(ctx, row.Path)
	if err == nil && meta != nil {
		entry.AccessCount = meta.AccessCount
	}

	return &entry, nil
}

// resolveEntry tries multiple strategies to find a note:
// 1. By short ID
// 2. By exact path
// 3. By title
func (s *BrainServiceImpl) resolveEntry(ctx context.Context, pathOrID string) (*storage.NoteRow, error) {
	// Try by short ID first (most common for API calls)
	row, err := s.storage.GetNoteByShortID(ctx, pathOrID)
	if err != nil {
		return nil, fmt.Errorf("lookup by short ID: %w", err)
	}
	if row != nil {
		return row, nil
	}

	// Try by exact path
	row, err = s.storage.GetNoteByPath(ctx, pathOrID)
	if err != nil {
		return nil, fmt.Errorf("lookup by path: %w", err)
	}
	if row != nil {
		return row, nil
	}

	// Try by title
	row, err = s.storage.GetNoteByTitle(ctx, pathOrID)
	if err != nil {
		return nil, fmt.Errorf("lookup by title: %w", err)
	}
	if row != nil {
		return row, nil
	}

	return nil, nil
}

// =============================================================================
// Update
// =============================================================================

// Update modifies an existing brain entry.
func (s *BrainServiceImpl) Update(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
	// Resolve the entry
	row, err := s.resolveEntry(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, api.ErrNotFound
	}

	// Read file from disk
	absPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(row.Path))
	fileContent, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", absPath, err)
	}

	// Parse frontmatter
	doc, err := frontmatter.Parse(string(fileContent))
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	fm := &doc.Frontmatter
	body := doc.Body

	// Apply field updates
	if req.Title != nil {
		fm.Title = frontmatter.SanitizeTitle(*req.Title)
	}
	if req.Status != nil {
		fm.Status = *req.Status
	}
	if req.Priority != nil {
		fm.Priority = *req.Priority
	}
	if req.Tags != nil {
		var sanitized []string
		for _, tag := range req.Tags {
			if st, ok := frontmatter.SanitizeTag(tag); ok {
				sanitized = append(sanitized, st)
			}
		}
		fm.Tags = sanitized
	}
	if req.DependsOn != nil {
		var sanitized []string
		for _, dep := range *req.DependsOn {
			if sd := frontmatter.SanitizeDependsOnEntry(dep); sd != "" {
				sanitized = append(sanitized, sd)
			}
		}
		fm.DependsOn = sanitized
	}

	// Schedule fields
	if req.Schedule != nil {
		fm.Schedule = *req.Schedule
	}
	if req.ScheduleEnabled != nil {
		fm.ScheduleEnabled = req.ScheduleEnabled
	}
	if req.NextRun != nil {
		fm.NextRun = *req.NextRun
	}
	if req.MaxRuns != nil {
		fm.MaxRuns = req.MaxRuns
	}
	if req.StartsAt != nil {
		fm.StartsAt = *req.StartsAt
	}
	if req.ExpiresAt != nil {
		fm.ExpiresAt = *req.ExpiresAt
	}

	// Git/execution fields
	if req.TargetWorkdir != nil {
		fm.TargetWorkdir = frontmatter.SanitizeSimpleValue(*req.TargetWorkdir)
	}
	if req.GitBranch != nil {
		fm.GitBranch = frontmatter.SanitizeSimpleValue(*req.GitBranch)
	}
	if req.MergeTargetBranch != nil {
		fm.MergeTargetBranch = *req.MergeTargetBranch
	}
	if req.MergePolicy != nil {
		fm.MergePolicy = *req.MergePolicy
	}
	if req.MergeStrategy != nil {
		fm.MergeStrategy = *req.MergeStrategy
	}
	if req.RemoteBranchPolicy != nil {
		fm.RemoteBranchPolicy = *req.RemoteBranchPolicy
	}
	if req.OpenPRBeforeMerge != nil {
		fm.OpenPRBeforeMerge = req.OpenPRBeforeMerge
	}
	if req.ExecutionMode != nil {
		fm.ExecutionMode = *req.ExecutionMode
	}
	if req.CompleteOnIdle != nil {
		fm.CompleteOnIdle = req.CompleteOnIdle
	}

	// Feature fields
	if req.FeatureID != nil {
		fm.FeatureID = *req.FeatureID
	}
	if req.FeaturePriority != nil {
		fm.FeaturePriority = *req.FeaturePriority
	}
	if req.FeatureDependsOn != nil {
		fm.FeatureDependsOn = *req.FeatureDependsOn
	}

	// Task execution fields
	if req.DirectPrompt != nil {
		fm.DirectPrompt = *req.DirectPrompt
	}
	if req.Agent != nil {
		fm.Agent = *req.Agent
	}
	if req.Model != nil {
		fm.Model = *req.Model
	}

	// Generated fields
	if req.Generated != nil {
		fm.Generated = req.Generated
	}
	if req.GeneratedKind != nil {
		fm.GeneratedKind = *req.GeneratedKind
	}
	if req.GeneratedKey != nil {
		fm.GeneratedKey = *req.GeneratedKey
	}
	if req.GeneratedBy != nil {
		fm.GeneratedBy = *req.GeneratedBy
	}

	// Sessions
	if req.Sessions != nil {
		if fm.Sessions == nil {
			fm.Sessions = make(map[string]frontmatter.SessionInfo)
		}
		for k, v := range req.Sessions {
			fm.Sessions[k] = frontmatter.SessionInfo{
				Timestamp: v.Timestamp,
				CronID:    v.CronID,
				RunID:     v.RunID,
			}
		}
	}

	// Runs
	if req.Runs != nil {
		fm.Runs = make([]frontmatter.CronRun, len(req.Runs))
		for i, r := range req.Runs {
			fm.Runs[i] = frontmatter.CronRun{
				RunID:      r.RunID,
				Status:     r.Status,
				Started:    r.Started,
				Completed:  r.Completed,
				SkipReason: r.SkipReason,
			}
			if r.Duration != nil {
				fm.Runs[i].Duration = fmt.Sprintf("%d", *r.Duration)
			}
			if r.Tasks != nil {
				fm.Runs[i].Tasks = fmt.Sprintf("%d", *r.Tasks)
			}
			if r.FailedTask != "" {
				fm.Runs[i].FailedTask = r.FailedTask
			}
		}
	}

	// RunFinalizations
	if req.RunFinalizations != nil {
		if fm.RunFinalizations == nil {
			fm.RunFinalizations = make(map[string]frontmatter.RunFinalization)
		}
		for k, v := range req.RunFinalizations {
			fm.RunFinalizations[k] = frontmatter.RunFinalization{
				Status:      v.Status,
				FinalizedAt: v.FinalizedAt,
				SessionID:   v.SessionID,
			}
		}
	}

	// Handle content replacement
	if req.Content != nil {
		body = *req.Content
	}

	// Handle append
	if req.Append != nil && *req.Append != "" {
		if body != "" {
			body = body + "\n\n" + *req.Append
		} else {
			body = *req.Append
		}
	}

	// Handle note (timestamped status change note)
	if req.Note != nil && *req.Note != "" {
		statusStr := fm.Status
		if req.Status != nil {
			statusStr = *req.Status
		}
		now := types.TimeNowUTC().Format(time.RFC3339)
		noteText := fmt.Sprintf("\n\n---\n*Status changed to **%s** on %s*\n\n%s", statusStr, now, *req.Note)
		body = body + noteText
	}

	// Serialize updated frontmatter and write back
	fmYAML := frontmatter.Serialize(fm)
	var fileBuilder strings.Builder
	fileBuilder.WriteString("---\n")
	fileBuilder.WriteString(fmYAML)
	fileBuilder.WriteString("---\n")
	if body != "" {
		fileBuilder.WriteString("\n")
		fileBuilder.WriteString(body)
		fileBuilder.WriteString("\n")
	}

	if err := os.WriteFile(absPath, []byte(fileBuilder.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write file %q: %w", absPath, err)
	}

	// Re-index
	if err := s.indexer.IndexFile(row.Path); err != nil {
		return nil, fmt.Errorf("re-index file %q: %w", row.Path, err)
	}

	// Re-read and return
	return s.Recall(ctx, row.Path)
}

// =============================================================================
// Delete
// =============================================================================

// Delete removes a brain entry by path or ID.
func (s *BrainServiceImpl) Delete(ctx context.Context, pathOrID string) error {
	row, err := s.resolveEntry(ctx, pathOrID)
	if err != nil {
		return err
	}
	if row == nil {
		return api.ErrNotFound
	}

	// Delete file from disk
	absPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(row.Path))
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file %q: %w", absPath, err)
	}

	// Remove from index
	if err := s.indexer.RemoveFile(row.Path); err != nil {
		return fmt.Errorf("remove from index %q: %w", row.Path, err)
	}

	return nil
}

// =============================================================================
// List
// =============================================================================

// List returns entries matching the given filters.
func (s *BrainServiceImpl) List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	opts := &storage.ListOptions{
		Type:      req.Type,
		Status:    req.Status,
		FeatureID: req.FeatureID,
		Limit:     req.Limit,
		Offset:    req.Offset,
		SortBy:    req.SortBy,
	}

	// Handle global vs project filtering
	if req.Global != nil && *req.Global {
		opts.PathPrefix = "global/"
	}

	// Handle tags (comma-separated string)
	if req.Tags != "" {
		tags := strings.Split(req.Tags, ",")
		var trimmed []string
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) > 0 {
			opts.Tags = trimmed
		}
	}

	rows, err := s.storage.ListNotes(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}

	// Apply filename filter if specified
	var filtered []*storage.NoteRow
	if req.Filename != "" {
		for _, row := range rows {
			filename := markdown.ExtractIDFromPath(row.Path)
			if markdown.MatchesFilenamePattern(filename, req.Filename) {
				filtered = append(filtered, row)
			}
		}
	} else {
		filtered = rows
	}

	entries := make([]types.BrainEntry, 0, len(filtered))
	for _, row := range filtered {
		entries = append(entries, NoteRowToBrainEntry(row))
	}

	total := len(entries)

	return &types.ListEntriesResponse{
		Entries: entries,
		Total:   total,
		Limit:   req.Limit,
		Offset:  req.Offset,
	}, nil
}

// =============================================================================
// Search
// =============================================================================

// Search performs full-text search across brain entries.
func (s *BrainServiceImpl) Search(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
	if req.Query == "" {
		return &types.SearchResponse{
			Results: []types.SearchResult{},
			Total:   0,
		}, nil
	}

	opts := &storage.SearchOptions{
		Type:   req.Type,
		Status: req.Status,
	}
	if req.Limit != nil {
		opts.Limit = *req.Limit
	}
	if req.Global != nil && *req.Global {
		opts.PathPrefix = "global/"
	}

	rows, err := s.storage.SearchNotes(ctx, req.Query, opts)
	if err != nil {
		return nil, fmt.Errorf("search notes: %w", err)
	}

	results := make([]types.SearchResult, 0, len(rows))
	for _, row := range rows {
		snippet := ""
		if row.Lead != nil {
			snippet = *row.Lead
		}
		results = append(results, types.SearchResult{
			ID:      row.ShortID,
			Path:    row.Path,
			Title:   row.Title,
			Type:    derefStr(row.Type),
			Status:  derefStr(row.Status),
			Snippet: snippet,
		})
	}

	return &types.SearchResponse{
		Results: results,
		Total:   len(results),
	}, nil
}

// =============================================================================
// Inject
// =============================================================================

// Inject returns formatted context for AI consumption.
func (s *BrainServiceImpl) Inject(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error) {
	if req.Query == "" {
		return &types.InjectResponse{
			Context: "",
			Entries: []types.InjectEntry{},
			Total:   0,
		}, nil
	}

	limit := 5
	if req.MaxEntries != nil && *req.MaxEntries > 0 {
		limit = *req.MaxEntries
	}

	opts := &storage.SearchOptions{
		Limit: limit,
		Type:  req.Type,
	}

	rows, err := s.storage.SearchNotes(ctx, req.Query, opts)
	if err != nil {
		return nil, fmt.Errorf("search for inject: %w", err)
	}

	var contextBuilder strings.Builder
	entries := make([]types.InjectEntry, 0, len(rows))

	for _, row := range rows {
		entry := NoteRowToBrainEntry(row)

		// Format as markdown context
		contextBuilder.WriteString("## ")
		contextBuilder.WriteString(entry.Title)
		contextBuilder.WriteString("\n")
		if entry.Content != "" {
			contextBuilder.WriteString(entry.Content)
			contextBuilder.WriteString("\n")
		}
		contextBuilder.WriteString("\n")

		entries = append(entries, types.InjectEntry{
			ID:    entry.ID,
			Path:  entry.Path,
			Title: entry.Title,
			Type:  entry.Type,
		})
	}

	return &types.InjectResponse{
		Context: strings.TrimSpace(contextBuilder.String()),
		Entries: entries,
		Total:   len(entries),
	}, nil
}

// =============================================================================
// Move
// =============================================================================

// Move moves an entry to a different project.
func (s *BrainServiceImpl) Move(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
	if targetProject == "" {
		return nil, fmt.Errorf("target project is required")
	}

	// Recall the entry
	entry, err := s.Recall(ctx, pathOrID)
	if err != nil {
		return nil, err
	}

	// Prevent moving in_progress tasks
	if entry.Status == "in_progress" {
		return nil, fmt.Errorf("cannot move entry with status 'in_progress'")
	}

	oldPath := entry.Path

	// Compute new path by replacing the project segment
	newPath, err := computeMovedPath(oldPath, targetProject)
	if err != nil {
		return nil, err
	}

	// Read old file content
	oldAbsPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(oldPath))
	content, err := os.ReadFile(oldAbsPath)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", oldAbsPath, err)
	}

	// Update project_id in frontmatter
	doc, err := frontmatter.Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	doc.Frontmatter.ProjectID = targetProject

	// Serialize updated content
	fmYAML := frontmatter.Serialize(&doc.Frontmatter)
	var fileBuilder strings.Builder
	fileBuilder.WriteString("---\n")
	fileBuilder.WriteString(fmYAML)
	fileBuilder.WriteString("---\n")
	if doc.Body != "" {
		fileBuilder.WriteString("\n")
		fileBuilder.WriteString(doc.Body)
		fileBuilder.WriteString("\n")
	}

	// Write to new path
	newAbsPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(newPath))
	newDir := filepath.Dir(newAbsPath)
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %q: %w", newDir, err)
	}
	if err := os.WriteFile(newAbsPath, []byte(fileBuilder.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write file %q: %w", newAbsPath, err)
	}

	// Delete old file
	if err := os.Remove(oldAbsPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove old file %q: %w", oldAbsPath, err)
	}

	// Update index
	if err := s.indexer.RemoveFile(oldPath); err != nil {
		return nil, fmt.Errorf("remove old index %q: %w", oldPath, err)
	}
	if err := s.indexer.IndexFile(newPath); err != nil {
		return nil, fmt.Errorf("index new file %q: %w", newPath, err)
	}

	return &types.MoveResult{
		Success: true,
		From:    oldPath,
		To:      newPath,
	}, nil
}

// computeMovedPath replaces the project segment in a path.
// "projects/old-project/task/abc12def.md" → "projects/new-project/task/abc12def.md"
// "global/task/abc12def.md" → "projects/new-project/task/abc12def.md"
func computeMovedPath(oldPath, targetProject string) (string, error) {
	parts := strings.Split(oldPath, "/")

	if len(parts) >= 3 && parts[0] == "projects" {
		// projects/<project>/<type>/<file>.md
		parts[1] = targetProject
		return strings.Join(parts, "/"), nil
	}

	if len(parts) >= 2 && parts[0] == "global" {
		// global/<type>/<file>.md → projects/<target>/<type>/<file>.md
		newParts := make([]string, 0, len(parts)+1)
		newParts = append(newParts, "projects", targetProject)
		newParts = append(newParts, parts[1:]...)
		return strings.Join(newParts, "/"), nil
	}

	return "", fmt.Errorf("cannot determine project from path %q", oldPath)
}

// =============================================================================
// Graph Operations
// =============================================================================

// GetBacklinks finds entries that link TO the given path.
func (s *BrainServiceImpl) GetBacklinks(ctx context.Context, path string) ([]types.BrainEntry, error) {
	noteRows, err := s.storage.GetBacklinks(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: %w", err)
	}
	return noteRowsToBrainEntries(noteRows), nil
}

// GetOutlinks finds entries linked BY the given path.
func (s *BrainServiceImpl) GetOutlinks(ctx context.Context, path string) ([]types.BrainEntry, error) {
	noteRows, err := s.storage.GetOutlinks(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get outlinks: %w", err)
	}
	return noteRowsToBrainEntries(noteRows), nil
}

// GetRelated finds entries sharing link targets (co-citation) with the given path.
func (s *BrainServiceImpl) GetRelated(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	noteRows, err := s.storage.GetRelated(ctx, path, limit)
	if err != nil {
		return nil, fmt.Errorf("get related: %w", err)
	}
	return noteRowsToBrainEntries(noteRows), nil
}

// =============================================================================
// Section Extraction
// =============================================================================

// GetSections extracts markdown section headers from an entry.
func (s *BrainServiceImpl) GetSections(ctx context.Context, path string) (*types.SectionsResponse, error) {
	entry, err := s.Recall(ctx, path)
	if err != nil {
		return nil, err
	}

	sections := extractSectionHeaders(entry.Content)

	return &types.SectionsResponse{
		Sections: sections,
		Path:     entry.Path,
	}, nil
}

// GetSection extracts a specific section's content by heading title.
func (s *BrainServiceImpl) GetSection(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
	entry, err := s.Recall(ctx, path)
	if err != nil {
		return nil, err
	}

	content, matchedTitle, found := extractSectionContent(entry.Content, title, includeSubsections)
	if !found {
		available := extractSectionHeaders(entry.Content)
		titles := make([]string, len(available))
		for i, s := range available {
			titles[i] = s.Title
		}
		return nil, fmt.Errorf("section %q not found; available sections: %s", title, strings.Join(titles, ", "))
	}

	return &types.SectionContentResponse{
		Title:              matchedTitle,
		Content:            content,
		Path:               entry.Path,
		IncludeSubsections: includeSubsections,
	}, nil
}

// =============================================================================
// Stats & Health
// =============================================================================

// GetStats returns aggregate statistics.
// When global=true, returns only global entries stats.
// When global=false, returns total stats across all entries.
func (s *BrainServiceImpl) GetStats(ctx context.Context, global bool) (*types.StatsResponse, error) {
	// Primary stats based on the global flag
	var primaryOpts *storage.StatsOptions
	if global {
		primaryOpts = &storage.StatsOptions{Path: "global/"}
	}

	primaryStats, err := s.storage.GetStats(ctx, primaryOpts)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	// Get global stats for the response field
	globalStats, err := s.storage.GetStats(ctx, &storage.StatsOptions{Path: "global/"})
	if err != nil {
		return nil, fmt.Errorf("get global stats: %w", err)
	}

	// Get project stats for the response field
	projectStats, err := s.storage.GetStats(ctx, &storage.StatsOptions{Path: "projects/"})
	if err != nil {
		return nil, fmt.Errorf("get project stats: %w", err)
	}

	return &types.StatsResponse{
		BrainDir:       s.config.BrainDir,
		DBPath:         filepath.Join(s.config.BrainDir, ".brain.db"),
		TotalEntries:   primaryStats.TotalNotes,
		GlobalEntries:  globalStats.TotalNotes,
		ProjectEntries: projectStats.TotalNotes,
		ByType:         primaryStats.ByType,
		OrphanCount:    primaryStats.OrphanCount,
		TrackedEntries: primaryStats.TrackedCount,
		StaleCount:     primaryStats.StaleCount,
	}, nil
}

// GetOrphans returns entries with no incoming links.
func (s *BrainServiceImpl) GetOrphans(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	noteRows, err := s.storage.GetOrphans(ctx, &storage.OrphanOptions{
		Type:  entryType,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get orphans: %w", err)
	}
	return noteRowsToBrainEntries(noteRows), nil
}

// GetStale returns entries not verified in N days.
func (s *BrainServiceImpl) GetStale(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error) {
	if days <= 0 {
		days = 30
	}
	if limit <= 0 {
		limit = 50
	}
	noteRows, err := s.storage.GetStaleEntries(ctx, days, &storage.StaleOptions{
		Type:  entryType,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get stale entries: %w", err)
	}
	return noteRowsToBrainEntries(noteRows), nil
}

// Verify marks an entry as verified. Resolves path/ID first to validate the entry exists.
func (s *BrainServiceImpl) Verify(ctx context.Context, path string) (*types.VerifyResponse, error) {
	// Resolve the entry to get the actual path and validate it exists
	entry, err := s.Recall(ctx, path)
	if err != nil {
		return nil, err
	}

	if err := s.storage.SetVerified(ctx, entry.Path); err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	return &types.VerifyResponse{
		Success:    true,
		Path:       entry.Path,
		VerifiedAt: types.TimeNowUTC().Format(time.RFC3339),
	}, nil
}

// =============================================================================
// Link Generation
// =============================================================================

// GenerateLink resolves an entry and returns a markdown link.
func (s *BrainServiceImpl) GenerateLink(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error) {
	lookupKey := req.Path
	if lookupKey == "" {
		lookupKey = req.Title
	}
	if lookupKey == "" {
		return nil, fmt.Errorf("path or title is required")
	}

	entry, err := s.Recall(ctx, lookupKey)
	if err != nil {
		return nil, err
	}

	linkTitle := entry.Title
	if req.WithTitle != nil && !*req.WithTitle {
		linkTitle = ""
	}

	link := markdown.GenerateMarkdownLink(entry.ID, linkTitle)

	return &types.LinkResponse{
		Link: link,
	}, nil
}

// =============================================================================
// Section Extraction Helpers
// =============================================================================

// extractSectionHeaders parses markdown body for heading lines (# through ######).
func extractSectionHeaders(body string) []types.SectionHeader {
	sections := make([]types.SectionHeader, 0)
	if body == "" {
		return sections
	}

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := line
		// Must start with # (at beginning of line)
		if len(trimmed) == 0 || trimmed[0] != '#' {
			continue
		}

		// Count leading # characters
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level > 6 {
			continue
		}

		// Must be followed by a space
		if level >= len(trimmed) || trimmed[level] != ' ' {
			continue
		}

		title := strings.TrimSpace(trimmed[level+1:])
		if title == "" {
			continue
		}

		sections = append(sections, types.SectionHeader{
			Title: title,
			Level: level,
		})
	}

	return sections
}

// extractSectionContent finds a section by case-insensitive substring match and extracts its content.
// Returns (content, matchedTitle, found).
func extractSectionContent(body string, searchTitle string, includeSubsections bool) (string, string, bool) {
	if body == "" {
		return "", "", false
	}

	lines := strings.Split(body, "\n")
	searchLower := strings.ToLower(searchTitle)

	// Find the matching heading
	startIdx := -1
	matchedTitle := ""
	matchedLevel := 0

	for i, line := range lines {
		if len(line) == 0 || line[0] != '#' {
			continue
		}

		level := 0
		for level < len(line) && line[level] == '#' {
			level++
		}
		if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
			continue
		}

		title := strings.TrimSpace(line[level+1:])
		if strings.Contains(strings.ToLower(title), searchLower) {
			startIdx = i
			matchedTitle = title
			matchedLevel = level
			break
		}
	}

	if startIdx < 0 {
		return "", "", false
	}

	// Collect content lines until next heading of same or higher level
	var contentLines []string
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]

		// Check if this is a heading
		if len(line) > 0 && line[0] == '#' {
			level := 0
			for level < len(line) && line[level] == '#' {
				level++
			}
			if level > 0 && level <= 6 && level < len(line) && line[level] == ' ' {
				// This is a heading
				if includeSubsections {
					// Stop only at same or higher level
					if level <= matchedLevel {
						break
					}
				} else {
					// Stop at any heading
					break
				}
			}
		}

		contentLines = append(contentLines, line)
	}

	content := strings.TrimSpace(strings.Join(contentLines, "\n"))
	return content, matchedTitle, true
}

// =============================================================================
// Helpers
// =============================================================================

// noteRowsToBrainEntries converts a slice of NoteRow pointers to BrainEntry slice.
// Returns a non-nil empty slice if input is empty.
func noteRowsToBrainEntries(rows []*storage.NoteRow) []types.BrainEntry {
	entries := make([]types.BrainEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, NoteRowToBrainEntry(row))
	}
	return entries
}

// derefStr safely dereferences a *string, returning "" for nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
