package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/types"
)

// maxDeleteProjectErrors caps the error list on a DeleteProjectResponse. A
// wipe that fails on every one of 900 entries has one cause, not 900 — the
// count tells the user the scale, the sample tells them the reason.
const maxDeleteProjectErrors = 25

// DeleteProject erases a project: every brain entry under it, its index rows,
// its project-scoped runtime state, and the projects/<id>/ directory itself.
//
// Why this is not BulkDelete with a project filter:
//
//   - BulkDelete caps at 100 entries per call and scans at most 500
//     candidates. A project with 400 entries would need five round trips and
//     would silently report `truncated` rather than finishing the job.
//   - Deleting every entry still leaves projects/<id>/task/ on disk, and
//     TaskServiceImpl.ListProjects lists a project by exactly that directory.
//     The project would come back empty in the sidebar forever.
//   - Claims, dispatch leases and pause state are keyed by project_id, not by
//     entry path, so no entry-level operation reaches them.
//
// Not transactional across the filesystem: entries are removed one at a time
// and a failure partway through leaves the earlier ones deleted. The response
// reports Deleted/Failed for exactly that reason — callers must surface a
// partial wipe rather than assuming all-or-nothing.
func (s *BrainServiceImpl) DeleteProject(ctx context.Context, projectID string) (*types.DeleteProjectResponse, error) {
	projectID = strings.TrimSpace(projectID)
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}

	projectDir := filepath.Join(s.config.BrainDir, "projects", projectID)

	// The project exists if EITHER its directory or an index row says so.
	// A project whose directory was removed out of band still has rows to
	// clean up, and refusing there would leave them stranded.
	indexedPaths, err := s.storage.ListProjectNotePaths(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project entries: %w", err)
	}
	dirInfo, statErr := os.Stat(projectDir)
	dirExists := statErr == nil && dirInfo.IsDir()
	if !dirExists && len(indexedPaths) == 0 {
		return nil, api.ErrNotFound
	}

	// Union of what the index knows and what is actually on disk. Either
	// source alone leaves debris: an unindexed file survives a purge driven
	// by the index, and an index row whose file is gone survives a purge
	// driven by the disk walk.
	paths := unionPaths(indexedPaths, s.markdownFilesUnder(projectDir, projectID))

	resp := &types.DeleteProjectResponse{Project: projectID}

	for _, p := range paths {
		if err := s.Delete(ctx, p); err != nil {
			// Already gone is the outcome we wanted, not a failure.
			if isMissingEntry(err) {
				continue
			}
			resp.Failed++
			if len(resp.Errors) < maxDeleteProjectErrors {
				resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", p, err))
			}
			continue
		}
		resp.Deleted++
	}

	// Sweep any index row the per-entry pass could not resolve — rows whose
	// file was already missing, which Delete reports as not-found.
	if n, err := s.storage.DeleteProjectNotes(ctx, projectID); err == nil {
		resp.IndexRowsRemoved = n
	} else if len(resp.Errors) < maxDeleteProjectErrors {
		resp.Errors = append(resp.Errors, fmt.Sprintf("index sweep: %v", err))
	}

	if removed, err := s.storage.PurgeProjectState(ctx, projectID); err == nil {
		resp.StateRowsRemoved = removed
	} else if len(resp.Errors) < maxDeleteProjectErrors {
		resp.Errors = append(resp.Errors, fmt.Sprintf("state purge: %v", err))
	}

	// The directory goes last. While it stands, ListProjects keeps listing
	// the project — so removing it only after the entries are gone means a
	// failed wipe leaves the project visible with whatever survived, rather
	// than hiding orphans behind a vanished name.
	//
	// Kept when entries failed to delete: the leftovers live in there.
	if resp.Failed == 0 {
		if err := os.RemoveAll(projectDir); err != nil {
			if len(resp.Errors) < maxDeleteProjectErrors {
				resp.Errors = append(resp.Errors, fmt.Sprintf("remove %s: %v", projectDir, err))
			}
		} else {
			resp.DirectoryRemoved = true
		}
	}

	// One project-level event, not one per entry. Delete already published
	// an entry.deleted per entry; a project.deleted on top of a few hundred
	// of those is the signal a UI actually subscribes to.
	s.publish(events.Event{
		Type:      events.ProjectDeleted,
		Source:    "service",
		ProjectID: projectID,
		Payload: map[string]any{
			"project":          projectID,
			"deleted":          resp.Deleted,
			"failed":           resp.Failed,
			"directoryRemoved": resp.DirectoryRemoved,
			"indexRowsRemoved": resp.IndexRowsRemoved,
		},
	})

	return resp, nil
}

// validateProjectID rejects ids that would let a delete escape the projects
// directory. This is the one operation in the service that removes a whole
// tree, so the check is here rather than only at the HTTP edge — a future
// caller (MCP, CLI, an automation) gets the same guarantee.
func validateProjectID(projectID string) error {
	if projectID == "" {
		return fmt.Errorf("project id required")
	}
	if projectID == "." || projectID == ".." {
		return fmt.Errorf("invalid project id %q", projectID)
	}
	if strings.ContainsAny(projectID, `/\`) || strings.Contains(projectID, "..") {
		return fmt.Errorf("invalid project id %q: must not contain path separators", projectID)
	}
	// filepath.Clean collapsing to something else means the id carried
	// structure a plain directory name would not.
	if filepath.Clean(projectID) != projectID {
		return fmt.Errorf("invalid project id %q", projectID)
	}
	return nil
}

// markdownFilesUnder returns brain-relative paths of every .md file under a
// project directory. Errors are swallowed: this is the belt to the index's
// braces, and a partially readable tree should still contribute what it can.
func (s *BrainServiceImpl) markdownFilesUnder(projectDir, projectID string) []string {
	var out []string
	prefix := filepath.Join("projects", projectID)
	_ = filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable subtree must not abort the walk
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(filepath.Join(prefix, rel)))
		return nil
	})
	return out
}

// unionPaths merges two path lists, de-duplicated and sorted.
func unionPaths(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, p := range list {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// isMissingEntry reports whether a Delete error means "it was already gone".
func isMissingEntry(err error) bool {
	return errors.Is(err, api.ErrNotFound) ||
		strings.Contains(strings.ToLower(err.Error()), "not found")
}
