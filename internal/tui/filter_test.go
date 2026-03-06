package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestFilterTasks(t *testing.T) {
	tasks := []types.ResolvedTask{
		{
			ID:    "abc123",
			Title: "Auth implementation",
		},
		{
			ID:    "def456",
			Title: "Database migration",
		},
		{
			ID:    "ghi789",
			Title: "Auth tests",
		},
		{
			ID:    "jkl012",
			Title: "Frontend styling",
		},
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{
			name:     "Empty query returns all tasks",
			query:    "",
			expected: 4,
		},
		{
			name:     "Filter by title (auth)",
			query:    "auth",
			expected: 2,
		},
		{
			name:     "Filter by title (database)",
			query:    "database",
			expected: 1,
		},
		{
			name:     "Filter by ID",
			query:    "abc",
			expected: 1,
		},
		{
			name:     "No matches",
			query:    "xyz",
			expected: 0,
		},
		{
			name:     "Case insensitive",
			query:    "AUTH",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := FilterTasks(tasks, tt.query)
			if len(filtered) != tt.expected {
				t.Errorf("FilterTasks(%q) = %d tasks, expected %d", tt.query, len(filtered), tt.expected)
			}
		})
	}
}

func TestMatchesQuery(t *testing.T) {
	task := types.ResolvedTask{
		ID:    "abc123",
		Title: "Auth Implementation",
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "Match title (lowercase query)",
			query:    "auth",
			expected: true,
		},
		{
			name:     "Match ID (lowercase query)",
			query:    "abc",
			expected: true,
		},
		{
			name:     "No match",
			query:    "xyz",
			expected: false,
		},
		{
			name:     "Partial match (lowercase query)",
			query:    "impl",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesQuery(task, tt.query)
			if result != tt.expected {
				t.Errorf("matchesQuery(%q) = %v, expected %v", tt.query, result, tt.expected)
			}
		})
	}
}
