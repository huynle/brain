package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func (s *BrainServiceImpl) entryRevision(ctx context.Context, row *storage.NoteRow) string {
	raw := ""
	if row.RawContent != nil {
		raw = *row.RawContent
	}
	if path, err := s.filesystemPath(ctx, row.Path, true); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			raw = string(b)
		}
	}
	var metadata any
	_ = json.Unmarshal([]byte(row.Metadata), &metadata)
	b, _ := json.Marshal(struct {
		Path, Content string
		Metadata      any
		Status        *string
	}{row.Path, raw, metadata, row.Status})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
func (s *BrainServiceImpl) checkEntryRevision(ctx context.Context, row *storage.NoteRow, expected string) error {
	if expected != "" && expected != s.entryRevision(ctx, row) {
		return fmt.Errorf("%w: entry revision changed; read the entry again before applying changes", api.ErrConflict)
	}
	return nil
}
func (s *BrainServiceImpl) validateDependencyUpdate(ctx context.Context, row *storage.NoteRow, req types.UpdateEntryRequest) error {
	if req.DependsOn == nil && req.FeatureDependsOn == nil {
		return nil
	}
	if row.ProjectID == nil || row.Type == nil || *row.Type != "task" {
		return nil
	}
	result, err := s.List(ctx, types.ListEntriesRequest{Project: *row.ProjectID, Type: "task", Limit: 10001})
	if err != nil {
		return err
	}
	if len(result.Entries) > 10000 {
		return fmt.Errorf("%w: graph validation exceeds 10000-task bound", api.ErrInvalidInput)
	}
	tasks := result.Entries
	lookup := BuildLookupMaps(tasks)
	task := lookup.ByID[row.ShortID]
	if task == nil {
		return api.ErrNotFound
	}
	if req.DependsOn != nil {
		task.DependsOn = *req.DependsOn
		for _, ref := range task.DependsOn {
			if ResolveDep(ref, lookup) == "" {
				return fmt.Errorf("%w: unknown dependency %s", api.ErrInvalidInput, ref)
			}
		}
	}
	if req.FeatureDependsOn != nil {
		task.FeatureDependsOn = *req.FeatureDependsOn
		features := map[string]bool{}
		for _, t := range tasks {
			features[t.FeatureID] = true
		}
		for _, id := range task.FeatureDependsOn {
			if !features[id] {
				return fmt.Errorf("%w: unknown feature dependency %s", api.ErrInvalidInput, id)
			}
		}
	}
	if FindCycles(BuildAdjacencyList(tasks, lookup))[row.ShortID] {
		return fmt.Errorf("%w: dependency cycle", api.ErrInvalidInput)
	}
	resolved := make([]types.ResolvedTask, 0, len(tasks))
	for i := range tasks {
		resolved = append(resolved, brainEntryToResolvedTask(&tasks[i]))
	}
	for _, feature := range ComputeAndResolveFeatures(resolved).Features {
		if feature.ID == task.FeatureID && feature.InCycle {
			return fmt.Errorf("%w: feature dependency cycle", api.ErrInvalidInput)
		}
	}
	return nil
}
