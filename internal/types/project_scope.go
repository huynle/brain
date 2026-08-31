package types

import "strings"

// GlobalProjectToken is the reserved member of a multi-project scope
// (ListEntriesRequest.Projects / SearchRequest.Projects) that admits
// project-less global/ entries alongside the named projects.
//
// It is a reserved token, not a project id: entries of a project literally
// named "global" live under projects/global/ and are unreachable by this
// scope. That trade is deliberate — the Entries browser's project picker
// already lists "global" next to real project ids, so one flat list is the
// shape callers actually have.
const GlobalProjectToken = "global"

// ParseProjectScope splits a caller-supplied project scope into the project
// ids to match on and whether global entries come along. Blanks and
// duplicates are dropped, so a scope built by concatenating UI state stays
// well-formed.
func ParseProjectScope(projects []string) (ids []string, includeGlobal bool) {
	seen := make(map[string]bool, len(projects))
	for _, p := range projects {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if p == GlobalProjectToken {
			includeGlobal = true
			continue
		}
		ids = append(ids, p)
	}
	return ids, includeGlobal
}

// SplitCommaScope parses the comma-separated form a query string carries
// (`?projects=a,b,global`) into the slice form the request types use.
func SplitCommaScope(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
