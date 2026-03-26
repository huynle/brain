package tui

import "strings"

// makeFeatureCollapseKey generates a collapse key for a feature.
// If statusName is empty, returns just the featureID (top-level active feature).
// If statusName is provided, returns "status:feature_id" (hierarchical key).
// The status name is lowercased for consistency.
func makeFeatureCollapseKey(statusName, featureID string) string {
	// Trim and check if statusName is empty
	statusName = strings.TrimSpace(statusName)
	if statusName == "" {
		return featureID
	}

	// Lowercase status name for consistency
	return strings.ToLower(statusName) + ":" + featureID
}

// isFeatureCollapsed checks if a feature is collapsed using hierarchical collapse keys.
// If statusName is empty, checks the top-level feature key.
// If statusName is provided, checks the hierarchical "status:feature_id" key.
// Returns false if the key is not in the map (default state is expanded).
func isFeatureCollapsed(statusName, featureID string, collapsedState map[string]bool) bool {
	if collapsedState == nil {
		return false
	}

	key := makeFeatureCollapseKey(statusName, featureID)
	return collapsedState[key]
}
