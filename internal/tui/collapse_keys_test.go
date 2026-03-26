package tui

import "testing"

func TestMakeFeatureCollapseKey(t *testing.T) {
	tests := []struct {
		name       string
		statusName string
		featureID  string
		want       string
	}{
		{
			name:       "hierarchical key for feature within status",
			statusName: "Completed",
			featureID:  "auth-system",
			want:       "completed:auth-system",
		},
		{
			name:       "hierarchical key with different status",
			statusName: "Ready",
			featureID:  "tui-parity",
			want:       "ready:tui-parity",
		},
		{
			name:       "top-level feature when status is empty",
			statusName: "",
			featureID:  "auth-system",
			want:       "auth-system",
		},
		{
			name:       "top-level feature when status is whitespace",
			statusName: "   ",
			featureID:  "onboarding",
			want:       "onboarding",
		},
		{
			name:       "lowercases status name",
			statusName: "BLOCKED",
			featureID:  "feature-123",
			want:       "blocked:feature-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeFeatureCollapseKey(tt.statusName, tt.featureID)
			if got != tt.want {
				t.Errorf("makeFeatureCollapseKey(%q, %q) = %q, want %q",
					tt.statusName, tt.featureID, got, tt.want)
			}
		})
	}
}

func TestIsFeatureCollapsed(t *testing.T) {
	tests := []struct {
		name           string
		statusName     string
		featureID      string
		collapsedState map[string]bool
		want           bool
	}{
		{
			name:       "hierarchical key - collapsed",
			statusName: "Completed",
			featureID:  "auth-system",
			collapsedState: map[string]bool{
				"completed:auth-system": true,
			},
			want: true,
		},
		{
			name:       "hierarchical key - not collapsed",
			statusName: "Completed",
			featureID:  "auth-system",
			collapsedState: map[string]bool{
				"completed:auth-system": false,
			},
			want: false,
		},
		{
			name:       "hierarchical key - not in map defaults to false",
			statusName: "Ready",
			featureID:  "tui-parity",
			collapsedState: map[string]bool{
				"completed:auth-system": true,
			},
			want: false,
		},
		{
			name:       "top-level key when status empty - collapsed",
			statusName: "",
			featureID:  "auth-system",
			collapsedState: map[string]bool{
				"auth-system": true,
			},
			want: true,
		},
		{
			name:       "top-level key when status empty - not collapsed",
			statusName: "",
			featureID:  "auth-system",
			collapsedState: map[string]bool{
				"auth-system": false,
			},
			want: false,
		},
		{
			name:           "empty collapse state map defaults to false",
			statusName:     "Completed",
			featureID:      "auth-system",
			collapsedState: map[string]bool{},
			want:           false,
		},
		{
			name:           "nil collapse state map defaults to false",
			statusName:     "Completed",
			featureID:      "auth-system",
			collapsedState: nil,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFeatureCollapsed(tt.statusName, tt.featureID, tt.collapsedState)
			if got != tt.want {
				t.Errorf("isFeatureCollapsed(%q, %q, state) = %v, want %v",
					tt.statusName, tt.featureID, got, tt.want)
			}
		})
	}
}
