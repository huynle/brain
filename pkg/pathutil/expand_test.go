package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde slash path",
			input:    "~/brain",
			expected: filepath.Join(home, "brain"),
		},
		{
			name:     "tilde alone",
			input:    "~",
			expected: home,
		},
		{
			name:     "tilde nested path",
			input:    "~/some/deep/path",
			expected: filepath.Join(home, "some", "deep", "path"),
		},
		{
			name:     "absolute path unchanged",
			input:    "/usr/local/bin",
			expected: "/usr/local/bin",
		},
		{
			name:     "relative path unchanged",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "tilde in middle unchanged",
			input:    "/some/~/path",
			expected: "/some/~/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandTilde(tt.input)
			if got != tt.expected {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
