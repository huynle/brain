package service

import (
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ProjectScopePaths renders a multi-project scope (see
// types.ParseProjectScope) as the storage path prefixes it covers — the form
// GetStats filters by. Returns nil for an empty scope, which means "no
// filter" to every caller.
func ProjectScopePaths(projects []string) []string {
	ids, includeGlobal := types.ParseProjectScope(projects)
	paths := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		paths = append(paths, "projects/"+id+"/")
	}
	if includeGlobal {
		paths = append(paths, storage.GlobalPathPrefix)
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}
