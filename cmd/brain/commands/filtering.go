package commands

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/huynle/brain-api/internal/types"
)

// BrainFilter is a composable struct embedded into CLI command structs for
// consistent filtering across all brain entry commands. Inspired by zk CLI patterns.
type BrainFilter struct {
	Type      string // --type (task, plan, note, etc.)
	Status    string // --status (active, pending, completed, etc.)
	Tags      string // --tags "api,auth" (comma-separated)
	Priority  string // --priority (high, medium, low)
	FeatureID string // --feature-id
	Limit     int    // --limit (default: 20)
	Sort      string // --sort (created, modified, priority)
	Match     string // --match / -m (search query for list command)
}

// validSortValues enumerates accepted --sort values.
var validSortValues = map[string]bool{
	"created":  true,
	"modified": true,
	"priority": true,
}

// RegisterFlags registers all filter flags on the given FlagSet.
func (f *BrainFilter) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&f.Type, "type", "", "Entry type (task, plan, note, etc.)")
	fs.StringVar(&f.Status, "status", "", "Entry status (active, pending, completed, etc.)")
	fs.StringVar(&f.Tags, "tags", "", "Comma-separated tags to filter by")
	fs.StringVar(&f.Priority, "priority", "", "Priority level (high, medium, low)")
	fs.StringVar(&f.FeatureID, "feature-id", "", "Feature ID to filter by")
	fs.IntVar(&f.Limit, "limit", 20, "Maximum number of results")
	fs.StringVar(&f.Sort, "sort", "", "Sort order (created, modified, priority)")
	fs.StringVar(&f.Match, "match", "", "Search query")
	fs.StringVar(&f.Match, "m", "", "Search query (short)")
}

// ToQueryParams converts the filter to a map of API query parameters.
// Empty/zero-value fields are omitted.
func (f *BrainFilter) ToQueryParams() map[string]string {
	params := make(map[string]string)

	if f.Type != "" {
		params["type"] = f.Type
	}
	if f.Status != "" {
		params["status"] = f.Status
	}
	if f.Tags != "" {
		params["tags"] = f.Tags
	}
	if f.Priority != "" {
		params["priority"] = f.Priority
	}
	if f.FeatureID != "" {
		params["feature_id"] = f.FeatureID
	}
	if f.Limit > 0 {
		params["limit"] = strconv.Itoa(f.Limit)
	}
	if f.Sort != "" {
		params["sortBy"] = f.Sort
	}
	if f.Match != "" {
		params["query"] = f.Match
	}

	return params
}

// Validate checks that all enum fields contain valid values.
// Empty values are considered valid (no filter applied).
func (f *BrainFilter) Validate() error {
	if f.Type != "" && !types.IsValidEntryType(f.Type) {
		return fmt.Errorf("invalid type %q; valid types: %v", f.Type, types.EntryTypes)
	}
	if f.Status != "" && !types.IsValidEntryStatus(f.Status) {
		return fmt.Errorf("invalid status %q; valid statuses: %v", f.Status, types.EntryStatuses)
	}
	if f.Priority != "" && !types.IsValidPriority(f.Priority) {
		return fmt.Errorf("invalid priority %q; valid priorities: %v", f.Priority, types.Priorities)
	}
	if f.Sort != "" && !validSortValues[f.Sort] {
		return fmt.Errorf("invalid sort %q; valid values: created, modified, priority", f.Sort)
	}
	return nil
}
