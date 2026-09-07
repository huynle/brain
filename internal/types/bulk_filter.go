package types

import "strings"

// IsEffective reports whether the bulk filter actually constrains List or its
// postfilters. Indexed empty strings disappear in storage; tags are split on
// commas and trimmed by List. Postfilters compare exactly, even against "".
func (f *BulkUpdateFilter) IsEffective() bool {
	if f == nil {
		return false
	}
	for _, value := range []*string{f.FeatureID, f.Project, f.Type, f.Status, f.Priority} {
		if value != nil && *value != "" {
			return true
		}
	}
	for _, tag := range f.Tags {
		for _, part := range strings.Split(tag, ",") {
			if strings.TrimSpace(part) != "" {
				return true
			}
		}
	}
	return f.GeneratedBy != nil || f.GeneratedKey != nil || f.Agent != nil ||
		f.Executor != nil || f.ExecutionMode != nil
}
