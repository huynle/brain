package storage

import (
	"fmt"
	"strings"
)

// GlobalPathPrefix is the path prefix under which project-less ("global")
// entries live. Global entries carry no project_id at all, so a
// `project_id IN (…)` filter can never reach them — they are addressed by
// path instead.
const GlobalPathPrefix = "global/"

// projectScopeClause renders the SQL predicate for a multi-project scope.
//
// The single-project filters (ListOptions.ProjectID, SearchOptions.ProjectID)
// answer "entries of exactly this project". This one answers "entries of any
// of these projects" — the shape the Entries browser needs, because the
// sidebar's visible-project set is a set, and the caller would otherwise have
// to fan out one query per project.
//
// includeGlobal additionally admits global/ entries. They belong to no
// project, so a scope that named only projects would silently drop them.
//
// Returns ("", nil) when the scope restricts nothing, so callers can append
// unconditionally.
func projectScopeClause(projectCol, pathCol string, ids []string, includeGlobal bool) (string, []interface{}) {
	if len(ids) == 0 && !includeGlobal {
		return "", nil
	}
	clauses := make([]string, 0, 2)
	params := make([]interface{}, 0, len(ids)+1)
	if len(ids) > 0 {
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			params = append(params, id)
		}
		clauses = append(clauses,
			fmt.Sprintf("%s IN (%s)", projectCol, strings.Join(placeholders, ",")))
	}
	if includeGlobal {
		clauses = append(clauses, pathCol+" LIKE ?")
		params = append(params, GlobalPathPrefix+"%")
	}
	return "(" + strings.Join(clauses, " OR ") + ")", params
}
