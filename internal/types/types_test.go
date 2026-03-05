package types

import "testing"

func TestIsValidEntryType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"summary", true},
		{"report", true},
		{"walkthrough", true},
		{"plan", true},
		{"pattern", true},
		{"learning", true},
		{"idea", true},
		{"scratch", true},
		{"decision", true},
		{"exploration", true},
		{"execution", true},
		{"task", true},
		{"invalid", false},
		{"", false},
		{"SUMMARY", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidEntryType(tt.input)
			if got != tt.want {
				t.Errorf("IsValidEntryType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidEntryStatus(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"draft", true},
		{"pending", true},
		{"active", true},
		{"in_progress", true},
		{"blocked", true},
		{"cancelled", true},
		{"completed", true},
		{"validated", true},
		{"superseded", true},
		{"archived", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidEntryStatus(tt.input)
			if got != tt.want {
				t.Errorf("IsValidEntryStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidPriority(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"high", true},
		{"medium", true},
		{"low", true},
		{"critical", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidPriority(tt.input)
			if got != tt.want {
				t.Errorf("IsValidPriority(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidTaskClassification(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ready", true},
		{"waiting", true},
		{"blocked", true},
		{"not_pending", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidTaskClassification(tt.input)
			if got != tt.want {
				t.Errorf("IsValidTaskClassification(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEntryTypeConstants(t *testing.T) {
	// Verify the count matches TypeScript source (12 types)
	if len(EntryTypes) != 12 {
		t.Errorf("expected 12 entry types, got %d", len(EntryTypes))
	}
}

func TestEntryStatusConstants(t *testing.T) {
	// Verify the count matches TypeScript source (10 statuses)
	if len(EntryStatuses) != 10 {
		t.Errorf("expected 10 entry statuses, got %d", len(EntryStatuses))
	}
}
