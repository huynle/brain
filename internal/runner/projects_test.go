package runner

import (
	"reflect"
	"testing"
)

func TestFilterProjects(t *testing.T) {
	tests := []struct {
		name     string
		projects []string
		include  []string
		exclude  []string
		expected []string
	}{
		{
			name:     "no filters returns all projects",
			projects: []string{"alpha", "beta", "gamma"},
			include:  nil,
			exclude:  nil,
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name:     "empty include and exclude returns all projects",
			projects: []string{"alpha", "beta", "gamma"},
			include:  []string{},
			exclude:  []string{},
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name:     "include only keeps matching projects",
			projects: []string{"prod-api", "prod-web", "staging-api", "dev-api"},
			include:  []string{"prod-*"},
			exclude:  nil,
			expected: []string{"prod-api", "prod-web"},
		},
		{
			name:     "exclude only removes matching projects",
			projects: []string{"prod-api", "prod-web", "staging-api", "dev-api"},
			include:  nil,
			exclude:  []string{"staging-*"},
			expected: []string{"prod-api", "prod-web", "dev-api"},
		},
		{
			name:     "include and exclude combined",
			projects: []string{"prod-api", "prod-legacy", "staging-api", "dev-api"},
			include:  []string{"prod-*"},
			exclude:  []string{"*-legacy"},
			expected: []string{"prod-api"},
		},
		{
			name:     "exclude wins over include on overlap",
			projects: []string{"prod-api", "prod-legacy"},
			include:  []string{"prod-*"},
			exclude:  []string{"prod-legacy"},
			expected: []string{"prod-api"},
		},
		{
			name:     "multiple include patterns match any",
			projects: []string{"prod-api", "staging-api", "dev-api", "test-api"},
			include:  []string{"prod-*", "staging-*"},
			exclude:  nil,
			expected: []string{"prod-api", "staging-api"},
		},
		{
			name:     "multiple exclude patterns remove any match",
			projects: []string{"prod-api", "staging-api", "dev-api", "test-api"},
			include:  nil,
			exclude:  []string{"staging-*", "test-*"},
			expected: []string{"prod-api", "dev-api"},
		},
		{
			name:     "wildcard question mark pattern",
			projects: []string{"test-a", "test-b", "test-ab", "prod-a"},
			include:  []string{"test-?"},
			exclude:  nil,
			expected: []string{"test-a", "test-b"},
		},
		{
			name:     "no include matches returns empty",
			projects: []string{"alpha", "beta", "gamma"},
			include:  []string{"prod-*"},
			exclude:  nil,
			expected: []string{},
		},
		{
			name:     "no exclude matches returns all",
			projects: []string{"alpha", "beta", "gamma"},
			include:  nil,
			exclude:  []string{"prod-*"},
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name:     "preserves original order",
			projects: []string{"charlie", "alpha", "bravo"},
			include:  []string{"*"},
			exclude:  nil,
			expected: []string{"charlie", "alpha", "bravo"},
		},
		{
			name:     "empty projects list returns empty",
			projects: []string{},
			include:  []string{"prod-*"},
			exclude:  []string{"test-*"},
			expected: []string{},
		},
		{
			name:     "nil projects list returns empty",
			projects: nil,
			include:  []string{"prod-*"},
			exclude:  nil,
			expected: []string{},
		},
		{
			name:     "invalid include pattern is skipped gracefully",
			projects: []string{"alpha", "beta"},
			include:  []string{"[invalid", "alpha"},
			exclude:  nil,
			expected: []string{"alpha"},
		},
		{
			name:     "invalid exclude pattern is skipped gracefully",
			projects: []string{"alpha", "beta"},
			include:  nil,
			exclude:  []string{"[invalid"},
			expected: []string{"alpha", "beta"},
		},
		{
			name:     "character class pattern",
			projects: []string{"proj-a", "proj-b", "proj-c", "proj-x"},
			include:  []string{"proj-[abc]"},
			exclude:  nil,
			expected: []string{"proj-a", "proj-b", "proj-c"},
		},
		{
			name:     "exact match include",
			projects: []string{"alpha", "beta", "gamma"},
			include:  []string{"beta"},
			exclude:  nil,
			expected: []string{"beta"},
		},
		{
			name:     "exclude everything results in empty",
			projects: []string{"alpha", "beta"},
			include:  nil,
			exclude:  []string{"*"},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterProjects(tt.projects, tt.include, tt.exclude)

			// Normalize nil to empty slice for comparison
			if tt.expected == nil {
				tt.expected = []string{}
			}
			if got == nil {
				got = []string{}
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("FilterProjects(%v, %v, %v) = %v, want %v",
					tt.projects, tt.include, tt.exclude, got, tt.expected)
			}
		})
	}
}
