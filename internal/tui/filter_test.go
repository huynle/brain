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

// =============================================================================
// New tests for FilterMode enum
// =============================================================================

func TestFilterModeString(t *testing.T) {
	tests := []struct {
		mode     FilterMode
		expected string
	}{
		{FilterOff, "off"},
		{FilterTyping, "typing"},
		{FilterLocked, "locked"},
		{FilterMode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.mode.String()
			if result != tt.expected {
				t.Errorf("FilterMode(%d).String() = %q, expected %q", tt.mode, result, tt.expected)
			}
		})
	}
}

func TestFilterModeIotaValues(t *testing.T) {
	// Verify iota ordering
	if FilterOff != 0 {
		t.Errorf("FilterOff = %d, expected 0", FilterOff)
	}
	if FilterTyping != 1 {
		t.Errorf("FilterTyping = %d, expected 1", FilterTyping)
	}
	if FilterLocked != 2 {
		t.Errorf("FilterLocked = %d, expected 2", FilterLocked)
	}
}

// =============================================================================
// Feature ID matching tests
// =============================================================================

func TestMatchesQueryFeatureID(t *testing.T) {
	task := types.ResolvedTask{
		ID:        "abc123",
		Title:     "Auth Implementation",
		FeatureID: "feat-login",
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "Match feature_id exact",
			query:    "feat-login",
			expected: true,
		},
		{
			name:     "Match feature_id partial",
			query:    "feat-",
			expected: true,
		},
		{
			name:     "Match feature_id case insensitive",
			query:    "FEAT-LOGIN",
			expected: true,
		},
		{
			name:     "No match feature_id",
			query:    "feat-signup",
			expected: false,
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

func TestFilterTasksByFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		{
			ID:        "abc123",
			Title:     "Auth implementation",
			FeatureID: "feat-login",
		},
		{
			ID:        "def456",
			Title:     "Database migration",
			FeatureID: "feat-db",
		},
		{
			ID:    "ghi789",
			Title: "Auth tests",
			// No feature_id
		},
		{
			ID:        "jkl012",
			Title:     "Login page",
			FeatureID: "feat-login",
		},
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{
			name:     "Filter by feature_id matches 2 tasks",
			query:    "feat-login",
			expected: 2,
		},
		{
			name:     "Filter by feature_id prefix matches all with features",
			query:    "feat-",
			expected: 3,
		},
		{
			name:     "Filter by feature_id matches 1 task",
			query:    "feat-db",
			expected: 1,
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

func TestMatchesQueryEmptyFeatureID(t *testing.T) {
	// Task with no feature_id should not match feature queries
	task := types.ResolvedTask{
		ID:    "abc123",
		Title: "Some task",
		// FeatureID is empty
	}

	result := matchesQuery(task, "feat-")
	if result != false {
		t.Errorf("matchesQuery with empty FeatureID should not match 'feat-', got %v", result)
	}
}

// =============================================================================
// Schedule/cron matching tests
// =============================================================================

func TestMatchesQuerySchedule(t *testing.T) {
	task := types.ResolvedTask{
		ID:       "abc123",
		Title:    "Check blocked tasks",
		Schedule: "*/15 * * * *",
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "Match 'cron' keyword",
			query:    "cron",
			expected: true,
		},
		{
			name:     "Match 'schedule' partial via 'scheduled' contains",
			query:    "sched",
			expected: true,
		},
		{
			name:     "Match 'scheduled' keyword",
			query:    "scheduled",
			expected: true,
		},
		{
			name:     "Match cron expression partial",
			query:    "*/15",
			expected: true,
		},
		{
			name:     "Match cron expression star",
			query:    "*",
			expected: true,
		},
		{
			name:     "No match unrelated query",
			query:    "xyz",
			expected: false,
		},
		{
			name:     "Case insensitive CRON",
			query:    "CRON",
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

func TestMatchesQueryNoSchedule(t *testing.T) {
	// Task without schedule should NOT match cron queries
	task := types.ResolvedTask{
		ID:    "abc123",
		Title: "Regular task",
		// Schedule is empty
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "cron does not match non-scheduled task",
			query:    "cron",
			expected: false,
		},
		{
			name:     "schedule does not match non-scheduled task",
			query:    "schedule",
			expected: false,
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

func TestFilterTasksSchedule(t *testing.T) {
	tasks := []types.ResolvedTask{
		{
			ID:       "abc123",
			Title:    "Scheduled check",
			Schedule: "*/15 * * * *",
		},
		{
			ID:    "def456",
			Title: "Regular task",
		},
		{
			ID:       "ghi789",
			Title:    "Nightly job",
			Schedule: "0 3 * * *",
		},
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{
			name:     "Filter 'cron' returns only scheduled tasks",
			query:    "cron",
			expected: 2,
		},
		{
			name:     "Filter by cron expression returns matching task",
			query:    "*/15",
			expected: 1,
		},
		{
			name:     "Filter 'regular' returns only title match",
			query:    "regular",
			expected: 1,
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
