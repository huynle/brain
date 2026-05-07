package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/embeddings"
	"github.com/huynle/brain-api/internal/events"
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
	bus     events.Bus
}

// NewBrainService creates a new BrainServiceImpl.
// The bus parameter is optional; if nil, no events are published.
func NewBrainService(cfg *config.Config, store *storage.StorageLayer, idx *indexer.Indexer, bus events.Bus) *BrainServiceImpl {
	return &BrainServiceImpl{
		config:  cfg,
		storage: store,
		indexer: idx,
		bus:     bus,
	}
}

// publish sends an event on the bus if one is configured.
func (s *BrainServiceImpl) publish(e events.Event) {
	if s.bus != nil {
		s.bus.Publish(e)
	}
}

// checkFeatureCompletion checks if all tasks in a feature are completed
// and publishes a feature.all_completed event if so.
func (s *BrainServiceImpl) checkFeatureCompletion(ctx context.Context, featureID, project string) {
	if featureID == "" {
		return
	}

	// Query all tasks in this feature
	listResp, err := s.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   project,
		FeatureID: featureID,
		Limit:     1000,
	})
	if err != nil || len(listResp.Entries) == 0 {
		return
	}

	// Convert to ResolvedTask for ComputeFeatureStatus
	resolved := make([]types.ResolvedTask, len(listResp.Entries))
	for i := range listResp.Entries {
		resolved[i] = brainEntryToResolvedTask(&listResp.Entries[i])
	}

	status := ComputeFeatureStatus(resolved)
	if status != "completed" {
		return
	}

	s.publish(events.Event{
		Type:      events.FeatureAllCompleted,
		Source:    "service",
		ProjectID: project,
		DedupKey:  "feature-completed:" + project + ":" + featureID,
		Payload: map[string]any{
			"feature_id": featureID,
			"project":    project,
			"task_count": len(listResp.Entries),
		},
	})
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
		FeatureSchedule:     req.FeatureSchedule,
		FeatureStartsAt:     req.FeatureStartsAt,
		FeatureExpiresAt:    req.FeatureExpiresAt,
		FeatureRunOnceAt:    req.FeatureRunOnceAt,
		FeatureTimezone:     req.FeatureTimezone,
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
		Executor:            req.Executor,
		Extensions:          req.Extensions,
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
		RunOnceAt:           req.RunOnceAt,
		Timezone:            req.Timezone,
		Trigger:             automationTriggerToFM(req.Trigger),
		Action:              automationActionToFM(req.Action),
		Retry:               automationRetryToFM(req.Retry),
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

	// Post-save: auto-create feature_schedule gate task if feature schedule fields are set
	if req.Type == "task" && req.FeatureID != "" {
		schedFields := extractFeatureScheduleFromCreate(req)
		if schedFields.HasAny() {
			if err := s.ensureFeatureScheduleGate(ctx, project, req.FeatureID, schedFields); err != nil {
				slog.Warn("failed to ensure feature schedule gate",
					"feature_id", req.FeatureID,
					"project", project,
					"error", err,
				)
			}
		}
	}

	resp := &types.CreateEntryResponse{
		ID:     shortID,
		Path:   relPath,
		Title:  title,
		Type:   req.Type,
		Status: status,
		Link:   link,
	}

	// Publish entry.created event
	s.publish(events.Event{
		Type:      events.EntryCreated,
		Source:    "service",
		ProjectID: project,
		Payload: map[string]any{
			"id":      shortID,
			"path":    relPath,
			"type":    req.Type,
			"project": project,
			"title":   title,
			"status":  status,
		},
	})

	return resp, nil
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
// RecallFull
// =============================================================================

// RecallFull returns the full raw file content (YAML frontmatter + markdown body) for an entry.
// This is used by the API layer when serving text/x-brain-full responses.
func (s *BrainServiceImpl) RecallFull(ctx context.Context, pathOrID string) (string, error) {
	if pathOrID == "" {
		return "", fmt.Errorf("path or ID is required")
	}

	row, err := s.resolveEntry(ctx, pathOrID)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", api.ErrNotFound
	}

	// NoteRow.RawContent stores the full file content (frontmatter + body).
	// It's populated by the indexer from the on-disk file.
	if row.RawContent != nil && *row.RawContent != "" {
		return *row.RawContent, nil
	}

	// Fallback: reconstruct from metadata + body
	return s.reconstructFullContent(row)
}

// reconstructFullContent rebuilds the full file content from a NoteRow's
// metadata JSON and body fields. This handles edge cases where RawContent
// was not populated (e.g., older index entries).
func (s *BrainServiceImpl) reconstructFullContent(row *storage.NoteRow) (string, error) {
	// Parse metadata JSON into a Frontmatter struct
	var fm frontmatter.Frontmatter
	if row.Metadata != "" && row.Metadata != "{}" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
			return "", fmt.Errorf("parse metadata: %w", err)
		}
		// Reconstruct frontmatter from metadata + row fields
		fm = reconstructFrontmatter(row, meta)
	} else {
		fm = reconstructFrontmatter(row, nil)
	}

	// Serialize frontmatter back to YAML
	fmYAML := frontmatter.Serialize(&fm)

	// Combine: ---\n{yaml}---\n[\n{body}\n]
	var buf strings.Builder
	buf.WriteString("---\n")
	buf.WriteString(fmYAML)
	buf.WriteString("---\n")
	if row.Body != nil && *row.Body != "" {
		buf.WriteString("\n")
		buf.WriteString(*row.Body)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}

// reconstructFrontmatter builds a Frontmatter struct from a NoteRow and its metadata map.
func reconstructFrontmatter(row *storage.NoteRow, meta map[string]interface{}) frontmatter.Frontmatter {
	fm := frontmatter.Frontmatter{
		Title: row.Title,
	}
	if row.Type != nil {
		fm.Type = *row.Type
	}
	if row.Status != nil {
		fm.Status = *row.Status
	}
	if row.Priority != nil {
		fm.Priority = *row.Priority
	}
	if row.ProjectID != nil {
		fm.ProjectID = *row.ProjectID
	}
	if row.FeatureID != nil {
		fm.FeatureID = *row.FeatureID
	}
	if row.Created != nil {
		fm.Created = *row.Created
	}

	// Pull additional fields from metadata JSON
	if meta != nil {
		if v, ok := meta["tags"]; ok {
			fm.Tags = metaToStringSlice(v)
		}
		if v, ok := meta["depends_on"]; ok {
			fm.DependsOn = metaToStringSlice(v)
		}
		if v, ok := meta["parent_id"].(string); ok {
			fm.ParentID = v
		}
		if v, ok := meta["workdir"].(string); ok {
			fm.Workdir = v
		}
		if v, ok := meta["git_remote"].(string); ok {
			fm.GitRemote = v
		}
		if v, ok := meta["git_branch"].(string); ok {
			fm.GitBranch = v
		}
		if v, ok := meta["user_original_request"].(string); ok {
			fm.UserOriginalRequest = v
		}
		if v, ok := meta["direct_prompt"].(string); ok {
			fm.DirectPrompt = v
		}
		if v, ok := meta["agent"].(string); ok {
			fm.Agent = v
		}
		if v, ok := meta["model"].(string); ok {
			fm.Model = v
		}
		if v, ok := meta["target_workdir"].(string); ok {
			fm.TargetWorkdir = v
		}
		if v, ok := meta["merge_target_branch"].(string); ok {
			fm.MergeTargetBranch = v
		}
		if v, ok := meta["merge_policy"].(string); ok {
			fm.MergePolicy = v
		}
		if v, ok := meta["merge_strategy"].(string); ok {
			fm.MergeStrategy = v
		}
		if v, ok := meta["remote_branch_policy"].(string); ok {
			fm.RemoteBranchPolicy = v
		}
		if v, ok := meta["execution_mode"].(string); ok {
			fm.ExecutionMode = v
		}
		if v, ok := meta["feature_priority"].(string); ok {
			fm.FeaturePriority = v
		}
		if v, ok := meta["feature_depends_on"]; ok {
			fm.FeatureDependsOn = metaToStringSlice(v)
		}
		if v, ok := meta["starts_at"].(string); ok {
			fm.StartsAt = v
		}
		if v, ok := meta["expires_at"].(string); ok {
			fm.ExpiresAt = v
		}
		if v, ok := meta["run_once_at"].(string); ok {
			fm.RunOnceAt = v
		}
		if v, ok := meta["timezone"].(string); ok {
			fm.Timezone = v
		}

		// Automation fields (nested maps from metadata JSON)
		if v, ok := meta["trigger"]; ok {
			if t := metaToAutomationTriggerFM(v); t != nil {
				fm.Trigger = t
			}
		}
		if v, ok := meta["action"]; ok {
			if a := metaToAutomationActionFM(v); a != nil {
				fm.Action = a
			}
		}
		if v, ok := meta["retry"]; ok {
			if r := metaToAutomationRetryFM(v); r != nil {
				fm.Retry = r
			}
		}
	}

	return fm
}

// metaToStringSlice converts a JSON value ([]interface{} or []string) to []string.
func metaToStringSlice(v interface{}) []string {
	switch items := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return items
	default:
		return nil
	}
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

	// Capture pre-update state for event derivation
	oldStatus := fm.Status

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
	if req.RunOnceAt != nil {
		fm.RunOnceAt = *req.RunOnceAt
	}
	if req.Timezone != nil {
		fm.Timezone = *req.Timezone
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
	if req.Executor != nil {
		fm.Executor = *req.Executor
	}
	if len(req.Extensions) > 0 {
		fm.Extensions = req.Extensions
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

	// Automation fields
	if req.Trigger != nil {
		fm.Trigger = automationTriggerToFM(req.Trigger)
	}
	if req.Action != nil {
		fm.Action = automationActionToFM(req.Action)
	}
	if req.Retry != nil {
		fm.Retry = automationRetryToFM(req.Retry)
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

	// Preserve runtime metadata from the DB that the filesystem doesn't track.
	// The re-index reads from disk which only has frontmatter fields, so any
	// runtime-only fields (sessions, schedule state, direct_prompt, etc.) would
	// be lost without this preservation step.
	var preservedFields map[string]interface{}
	runtimeKeys := []string{
		"sessions", "next_run", "schedule", "schedule_enabled",
		"complete_on_idle", "direct_prompt", "runs", "max_runs",
		"starts_at", "expires_at", "run_once_at", "timezone",
	}
	if row.Metadata != "" && row.Metadata != "{}" {
		var existingMeta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &existingMeta); err == nil {
			preservedFields = make(map[string]interface{})
			for _, key := range runtimeKeys {
				if val, ok := existingMeta[key]; ok {
					preservedFields[key] = val
				}
			}
			if len(preservedFields) == 0 {
				preservedFields = nil
			}
		}
	}

	// Re-index
	if err := s.indexer.IndexFile(row.Path); err != nil {
		return nil, fmt.Errorf("re-index file %q: %w", row.Path, err)
	}

	// Restore preserved runtime fields into the re-indexed metadata
	if preservedFields != nil {
		_, _ = s.storage.MergeMetadata(ctx, row.Path, preservedFields)
	}

	// Post-update: auto-create/update feature_schedule gate task if feature schedule fields are set
	if fm.Type == "task" && fm.FeatureID != "" {
		schedFields, hasFeatureSched := extractFeatureScheduleFromUpdate(req)
		if hasFeatureSched && schedFields.HasAny() {
			project := extractProjectFromPath(row.Path)
			if project != "" {
				if err := s.ensureFeatureScheduleGate(ctx, project, fm.FeatureID, schedFields); err != nil {
					slog.Warn("failed to ensure feature schedule gate on update",
						"feature_id", fm.FeatureID,
						"project", project,
						"error", err,
					)
				}
			}
		}
	}

	// Publish entry.updated event
	project := extractProjectFromPath(row.Path)
	var changes []string
	if req.Title != nil {
		changes = append(changes, "title")
	}
	if req.Status != nil {
		changes = append(changes, "status")
	}
	if req.Priority != nil {
		changes = append(changes, "priority")
	}
	if req.Tags != nil {
		changes = append(changes, "tags")
	}
	if req.DependsOn != nil {
		changes = append(changes, "depends_on")
	}
	if req.Append != nil {
		changes = append(changes, "append")
	}
	if req.Content != nil {
		changes = append(changes, "content")
	}
	if req.Note != nil {
		changes = append(changes, "note")
	}
	s.publish(events.Event{
		Type:      events.EntryUpdated,
		Source:    "service",
		ProjectID: project,
		Payload: map[string]any{
			"id":      row.ShortID,
			"path":    row.Path,
			"type":    fm.Type,
			"project": project,
			"changes": changes,
		},
	})

	// Derive task lifecycle events from status transitions
	if fm.Type == "task" && req.Status != nil {
		newStatus := *req.Status
		switch {
		case newStatus == "completed" && oldStatus != "completed":
			s.publish(events.Event{
				Type:      events.TaskCompleted,
				Source:    "service",
				ProjectID: project,
				Payload: map[string]any{
					"id":         row.ShortID,
					"path":       row.Path,
					"project":    project,
					"old_status": oldStatus,
				},
			})

			// Check if all tasks in this feature are now complete
			s.checkFeatureCompletion(ctx, fm.FeatureID, project)

		case (newStatus == "cancelled" || newStatus == "blocked") && oldStatus != newStatus:
			s.publish(events.Event{
				Type:      events.TaskFailed,
				Source:    "service",
				ProjectID: project,
				Payload: map[string]any{
					"id":         row.ShortID,
					"path":       row.Path,
					"project":    project,
					"old_status": oldStatus,
					"new_status": newStatus,
				},
			})
		}
	}

	// Re-read and return
	return s.Recall(ctx, row.Path)
}

// BulkUpdate applies updates to multiple entries in a single request.
// Supports two modes:
//   - Filter mode: Filter + Updates selects entries by criteria, applies shared updates.
//   - Explicit mode: Entries provides per-entry paths with individual updates.
//
// Safety cap (default 100, max 100) limits how many entries are affected.
// DryRun returns matched entries without applying changes.
// Partial failures are collected; successful updates are NOT rolled back.
func (s *BrainServiceImpl) BulkUpdate(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
	// 1. Validate request: must be (filter+updates) XOR entries, not both.
	hasFilter := req.Filter != nil
	hasEntries := len(req.Entries) > 0
	if hasFilter && hasEntries {
		return nil, fmt.Errorf("cannot specify both filter and entries")
	}
	if !hasFilter && !hasEntries {
		return nil, fmt.Errorf("must specify either filter or entries")
	}
	if hasFilter && req.Updates == nil {
		return nil, fmt.Errorf("updates required when using filter mode")
	}

	// 2. Apply safety cap: default 100, max 100.
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	// 3. Resolve target entries.
	type target struct {
		path    string
		updates types.UpdateEntryRequest
	}
	var targets []target

	if hasFilter {
		// Filter mode: query storage using existing List functionality.
		listReq := types.ListEntriesRequest{
			Limit: limit,
		}
		if req.Filter.FeatureID != nil {
			listReq.FeatureID = *req.Filter.FeatureID
		}
		if req.Filter.Project != nil {
			listReq.Project = *req.Filter.Project
		}
		if req.Filter.Type != nil {
			listReq.Type = *req.Filter.Type
		}
		if req.Filter.Status != nil {
			listReq.Status = *req.Filter.Status
		}
		if req.Filter.Priority != nil {
			listReq.Priority = *req.Filter.Priority
		}
		if len(req.Filter.Tags) > 0 {
			listReq.Tags = strings.Join(req.Filter.Tags, ",")
		}

		listResp, err := s.List(ctx, listReq)
		if err != nil {
			return nil, fmt.Errorf("filter query: %w", err)
		}

		for _, entry := range listResp.Entries {
			targets = append(targets, target{
				path:    entry.Path,
				updates: *req.Updates,
			})
		}
	} else {
		// Explicit mode: use provided entries directly.
		for _, entry := range req.Entries {
			targets = append(targets, target{
				path:    entry.Path,
				updates: entry.Updates,
			})
		}
	}

	// 4. Cap results at limit.
	if len(targets) > limit {
		targets = targets[:limit]
	}

	// 5. Dry run: return matched entries without changes.
	if req.DryRun {
		results := make([]types.BulkUpdateResult, 0, len(targets))
		for _, t := range targets {
			// Resolve each entry to get ID and title.
			row, err := s.resolveEntry(ctx, t.path)
			if err != nil || row == nil {
				results = append(results, types.BulkUpdateResult{
					Path:   t.path,
					Status: "error",
					Error:  "entry not found",
				})
				continue
			}
			results = append(results, types.BulkUpdateResult{
				Path:   row.Path,
				ID:     row.ShortID,
				Title:  row.Title,
				Status: "ok",
			})
		}
		return &types.BulkUpdateResponse{
			Total:   len(results),
			DryRun:  true,
			Results: results,
		}, nil
	}

	// 6. Apply updates: loop entries, call existing Update() per entry.
	results := make([]types.BulkUpdateResult, 0, len(targets))
	updated := 0
	failed := 0

	for _, t := range targets {
		entry, err := s.Update(ctx, t.path, t.updates)
		if err != nil {
			failed++
			results = append(results, types.BulkUpdateResult{
				Path:   t.path,
				Status: "error",
				Error:  err.Error(),
			})
			continue
		}
		updated++
		results = append(results, types.BulkUpdateResult{
			Path:   entry.Path,
			ID:     entry.ID,
			Title:  entry.Title,
			Status: "ok",
		})
	}

	// 7. Return aggregate response.
	return &types.BulkUpdateResponse{
		Updated: updated,
		Failed:  failed,
		Total:   len(results),
		DryRun:  false,
		Results: results,
	}, nil
}

// extractProjectFromPath extracts the project name from a brain entry path.
// Expected format: "projects/<project>/task/<id>.md"
func extractProjectFromPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

// =============================================================================
// Metadata Updates
// =============================================================================

// durableMetadataFields is the set of metadata fields that should be persisted
// back to the markdown file on disk. Fields not in this set are considered
// transient/runtime-only and live only in SQLite.
var durableMetadataFields = map[string]bool{
	"status":             true,
	"priority":           true,
	"tags":               true,
	"depends_on":         true,
	"title":              true,
	"feature_id":         true,
	"feature_priority":   true,
	"feature_depends_on": true,
	"note":               true,
	"append":             true,
	"starts_at":          true,
	"expires_at":         true,
	"run_once_at":        true,
	"timezone":           true,
}

// UpdateMetadata performs a shallow merge of fields into the entry's metadata
// JSON column in SQLite. If any "durable" fields are present (status, priority,
// tags, depends_on, title, feature_id, feature_priority, feature_depends_on,
// note, append), the changes are also written back to the markdown file on disk
// and the file is re-indexed.
func (s *BrainServiceImpl) UpdateMetadata(ctx context.Context, pathOrID string, fields map[string]interface{}) (*types.BrainEntry, error) {
	row, err := s.resolveEntry(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, api.ErrNotFound
	}

	// Check if any durable fields are present
	hasDurable := false
	for key := range fields {
		if durableMetadataFields[key] {
			hasDurable = true
			break
		}
	}

	// If durable fields are present, write changes to the markdown file
	if hasDurable {
		if err := s.syncDurableFieldsToFile(ctx, row, fields); err != nil {
			// Log warning but continue with DB update — file write failure
			// should not block the metadata update
			fmt.Fprintf(os.Stderr, "WARNING: failed to sync durable fields to file %q: %v\n", row.Path, err)
		}
	}

	// Always do the DB update (existing behavior)
	updated, err := s.storage.MergeMetadata(ctx, row.Path, fields)
	if err != nil {
		return nil, fmt.Errorf("merge metadata: %w", err)
	}
	if updated == nil {
		return nil, api.ErrNotFound
	}

	// Publish entry.updated event for metadata changes
	project := extractProjectFromPath(row.Path)
	var changes []string
	for k := range fields {
		changes = append(changes, k)
	}
	var entryType string
	if row.Type != nil {
		entryType = *row.Type
	}
	s.publish(events.Event{
		Type:      events.EntryUpdated,
		Source:    "service",
		ProjectID: project,
		Payload: map[string]any{
			"id":       row.ShortID,
			"path":     row.Path,
			"type":     entryType,
			"project":  project,
			"changes":  changes,
			"metadata": true,
		},
	})

	entry := NoteRowToBrainEntry(updated)
	return &entry, nil
}

// syncDurableFieldsToFile reads the markdown file, applies durable field
// changes to the frontmatter, writes the file back, and re-indexes it.
// This follows the same pattern as the Update() method.
func (s *BrainServiceImpl) syncDurableFieldsToFile(ctx context.Context, row *storage.NoteRow, fields map[string]interface{}) error {
	// Read file from disk
	absPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(row.Path))
	fileContent, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file %q: %w", absPath, err)
	}

	// Parse frontmatter
	doc, err := frontmatter.Parse(string(fileContent))
	if err != nil {
		return fmt.Errorf("parse frontmatter: %w", err)
	}

	fm := &doc.Frontmatter
	body := doc.Body

	// Apply durable field changes to frontmatter
	if v, ok := fields["status"]; ok {
		if s, ok := v.(string); ok {
			fm.Status = s
		}
	}
	if v, ok := fields["priority"]; ok {
		if s, ok := v.(string); ok {
			fm.Priority = s
		}
	}
	if v, ok := fields["title"]; ok {
		if s, ok := v.(string); ok {
			fm.Title = frontmatter.SanitizeTitle(s)
		}
	}
	if v, ok := fields["feature_id"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureID = s
		}
	}
	if v, ok := fields["feature_priority"]; ok {
		if s, ok := v.(string); ok {
			fm.FeaturePriority = s
		}
	}
	if v, ok := fields["feature_schedule"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureSchedule = s
		}
	}
	if v, ok := fields["feature_starts_at"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureStartsAt = s
		}
	}
	if v, ok := fields["feature_expires_at"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureExpiresAt = s
		}
	}
	if v, ok := fields["feature_run_once_at"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureRunOnceAt = s
		}
	}
	if v, ok := fields["feature_timezone"]; ok {
		if s, ok := v.(string); ok {
			fm.FeatureTimezone = s
		}
	}
	if v, ok := fields["starts_at"]; ok {
		if s, ok := v.(string); ok {
			fm.StartsAt = s
		}
	}
	if v, ok := fields["expires_at"]; ok {
		if s, ok := v.(string); ok {
			fm.ExpiresAt = s
		}
	}
	if v, ok := fields["run_once_at"]; ok {
		if s, ok := v.(string); ok {
			fm.RunOnceAt = s
		}
	}
	if v, ok := fields["timezone"]; ok {
		if s, ok := v.(string); ok {
			fm.Timezone = s
		}
	}

	// Slice fields: coerce from JSON's []interface{} or []string, then sanitize
	if v, ok := fields["tags"]; ok {
		fm.Tags = coerceStringSlice(v, func(s string) (string, bool) {
			return frontmatter.SanitizeTag(s)
		})
	}
	if v, ok := fields["depends_on"]; ok {
		fm.DependsOn = coerceStringSlice(v, func(s string) (string, bool) {
			sd := frontmatter.SanitizeDependsOnEntry(s)
			return sd, sd != ""
		})
	}
	if v, ok := fields["feature_depends_on"]; ok {
		fm.FeatureDependsOn = coerceStringSlice(v, func(s string) (string, bool) {
			sd := frontmatter.SanitizeDependsOnEntry(s)
			return sd, sd != ""
		})
	}

	// Handle append (add to body)
	if v, ok := fields["append"]; ok {
		if appendStr, ok := v.(string); ok && appendStr != "" {
			if body != "" {
				body = body + "\n\n" + appendStr
			} else {
				body = appendStr
			}
		}
	}

	// Handle note (timestamped status change note, appended to body)
	if v, ok := fields["note"]; ok {
		if noteStr, ok := v.(string); ok && noteStr != "" {
			statusStr := fm.Status
			if sv, ok := fields["status"]; ok {
				if s, ok := sv.(string); ok {
					statusStr = s
				}
			}
			now := types.TimeNowUTC().Format(time.RFC3339)
			noteText := fmt.Sprintf("\n\n---\n*Status changed to **%s** on %s*\n\n%s", statusStr, now, noteStr)
			body = body + noteText
		}
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
		return fmt.Errorf("write file %q: %w", absPath, err)
	}

	// Preserve runtime metadata from the DB that the filesystem doesn't track.
	// Same pattern as Update() method.
	var preservedFields map[string]interface{}
	runtimeKeys := []string{
		"sessions", "next_run", "schedule", "schedule_enabled",
		"complete_on_idle", "direct_prompt", "runs", "max_runs",
		"starts_at", "expires_at", "run_once_at", "timezone",
	}
	if row.Metadata != "" && row.Metadata != "{}" {
		var existingMeta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &existingMeta); err == nil {
			preservedFields = make(map[string]interface{})
			for _, key := range runtimeKeys {
				if val, ok := existingMeta[key]; ok {
					preservedFields[key] = val
				}
			}
			if len(preservedFields) == 0 {
				preservedFields = nil
			}
		}
	}

	// Re-index the file
	if err := s.indexer.IndexFile(row.Path); err != nil {
		return fmt.Errorf("re-index file %q: %w", row.Path, err)
	}

	// Restore preserved runtime fields into the re-indexed metadata
	if preservedFields != nil {
		_, _ = s.storage.MergeMetadata(ctx, row.Path, preservedFields)
	}

	return nil
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

	// Capture metadata for event before deletion
	delPath := row.Path
	delID := row.ShortID
	var delType string
	if row.Type != nil {
		delType = *row.Type
	}
	delProject := extractProjectFromPath(delPath)

	// Delete file from disk
	absPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(row.Path))
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file %q: %w", absPath, err)
	}

	// Remove from index
	if err := s.indexer.RemoveFile(row.Path); err != nil {
		return fmt.Errorf("remove from index %q: %w", row.Path, err)
	}

	// Publish entry.deleted event
	s.publish(events.Event{
		Type:      events.EntryDeleted,
		Source:    "service",
		ProjectID: delProject,
		Payload: map[string]any{
			"id":      delID,
			"path":    delPath,
			"type":    delType,
			"project": delProject,
		},
	})

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
		ProjectID: req.Project,
		Limit:     req.Limit,
		Offset:    req.Offset,
		SortBy:    req.SortBy,
		SortOrder: req.SortOrder,
		Priority:  req.Priority,
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
		Type:      req.Type,
		Status:    req.Status,
		ProjectID: req.Project,
		FeatureID: req.FeatureID,
		Tags:      req.Tags,
		Strategy:  req.Strategy,
		Priority:  req.Priority,
	}
	if req.Limit != nil {
		opts.Limit = *req.Limit
	}
	if req.Global != nil && *req.Global {
		opts.PathPrefix = "global/"
	}

	rows, err := s.searchRows(ctx, req.Query, opts)
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

func (s *BrainServiceImpl) searchRows(ctx context.Context, query string, opts *storage.SearchOptions) ([]*storage.NoteRow, error) {
	if opts == nil || opts.Strategy != "semantic" {
		return s.storage.SearchNotes(ctx, query, opts)
	}

	cfg := s.config.Embeddings.Normalize()
	if !cfg.Enabled {
		return nil, errors.New("semantic search requires embeddings to be enabled and configured")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("semantic search requires embeddings: %w", err)
	}

	var embedder embeddings.Embedder
	switch cfg.Provider {
	case "ollama":
		ollamaEmbedder, err := embeddings.NewOllamaEmbedder(embeddings.OllamaConfig{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			Timeout: time.Duration(cfg.TimeoutMS) * time.Millisecond,
		})
		if err != nil {
			return nil, fmt.Errorf("semantic search initialize ollama embedder: %w", err)
		}
		embedder = ollamaEmbedder
	default:
		return nil, fmt.Errorf("semantic search unsupported embedding provider %q", cfg.Provider)
	}

	vectors, err := embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("semantic search embed query: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("semantic search embed query returned %d vectors for 1 input", len(vectors))
	}
	if len(vectors[0]) == 0 {
		return nil, errors.New("semantic search embed query returned empty vector")
	}

	semanticOpts := *opts
	semanticOpts.SemanticModel = cfg.Model
	semanticOpts.SemanticVector = vectors[0]
	return s.storage.SearchNotes(ctx, query, &semanticOpts)
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
//
// Safety: This method is defensive against data loss. It verifies the source
// file exists on disk before proceeding, verifies the destination was written
// correctly before deleting the source, and treats index update failures as
// non-fatal (they self-heal on re-index).
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

	oldAbsPath := filepath.Join(s.config.BrainDir, filepath.FromSlash(oldPath))

	// SAFETY: Verify source file exists on disk (not just in DB).
	// This catches stale DB entries where the file was already removed.
	if _, err := os.Stat(oldAbsPath); err != nil {
		return nil, fmt.Errorf("source file does not exist on disk: %w", err)
	}

	// Read old file content
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

	// SAFETY: Verify destination was written correctly before deleting source.
	dstInfo, err := os.Stat(newAbsPath)
	if err != nil {
		return nil, fmt.Errorf("destination file verification failed after write: %w", err)
	}
	expectedSize := int64(len(fileBuilder.String()))
	if dstInfo.Size() != expectedSize {
		return nil, fmt.Errorf("destination file size mismatch: wrote %d bytes but file is %d bytes", expectedSize, dstInfo.Size())
	}

	// SAFETY: Delete old file ONLY after verifying destination exists.
	if err := os.Remove(oldAbsPath); err != nil && !os.IsNotExist(err) {
		// Destination exists but couldn't remove source — not data loss, just cleanup issue
		slog.Error("move: failed to remove source file (destination is safe)", "src", oldAbsPath, "dst", newAbsPath, "error", err)
		// Continue — the move is effectively complete, just has a leftover source file
	}

	// Update index — log errors but don't fail since files are already moved on disk.
	// Index inconsistencies self-heal on re-index.
	if err := s.indexer.RemoveFile(oldPath); err != nil {
		slog.Error("move: failed to remove old index entry (will self-heal on re-index)", "path", oldPath, "error", err)
	}
	if err := s.indexer.IndexFile(newPath); err != nil {
		slog.Error("move: failed to index new file (will self-heal on re-index)", "path", newPath, "error", err)
	}

	result := &types.MoveResult{
		Success: true,
		From:    oldPath,
		To:      newPath,
		OldPath: oldPath,
		NewPath: newPath,
		Project: targetProject,
		ID:      entry.ID,
		Title:   entry.Title,
	}

	// Publish entry.deleted for the source project and entry.created for the target
	srcProject := extractProjectFromPath(oldPath)
	if srcProject != "" {
		s.publish(events.Event{
			Type:      events.EntryDeleted,
			Source:    "service",
			ProjectID: srcProject,
			Payload: map[string]any{
				"id":      entry.ID,
				"path":    oldPath,
				"type":    entry.Type,
				"project": srcProject,
				"reason":  "moved",
			},
		})
	}
	s.publish(events.Event{
		Type:      events.EntryCreated,
		Source:    "service",
		ProjectID: targetProject,
		Payload: map[string]any{
			"id":      entry.ID,
			"path":    newPath,
			"type":    entry.Type,
			"project": targetProject,
			"reason":  "moved",
		},
	})

	return result, nil
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

// coerceStringSlice converts a value from JSON unmarshalling (either
// []interface{} or []string) into a sanitized []string. The sanitize function
// returns (sanitized, ok) — items where ok is false are dropped.
func coerceStringSlice(v interface{}, sanitize func(string) (string, bool)) []string {
	var raw []string
	switch items := v.(type) {
	case []interface{}:
		for _, item := range items {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	case []string:
		raw = items
	default:
		return nil
	}

	var result []string
	for _, s := range raw {
		if sanitized, ok := sanitize(s); ok {
			result = append(result, sanitized)
		}
	}
	return result
}
