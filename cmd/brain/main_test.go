package main

import (
	"reflect"
	"testing"
)

func TestRedirectLegacyInvocation(t *testing.T) {
	tests := []struct {
		name     string
		argv0    string
		args     []string
		expected []string
	}{
		{
			name:     "brain-api redirect",
			argv0:    "brain-api",
			args:     []string{"--port", "3000"},
			expected: []string{"server", "--port", "3000"},
		},
		{
			name:     "brain-api no args",
			argv0:    "brain-api",
			args:     []string{},
			expected: []string{"server"},
		},
		{
			name:     "brain-mcp redirect",
			argv0:    "brain-mcp",
			args:     []string{},
			expected: []string{"mcp"},
		},
		{
			name:     "brain-mcp with flags",
			argv0:    "brain-mcp",
			args:     []string{"--port", "8080"},
			expected: []string{"mcp", "--port", "8080"},
		},
		{
			name:     "brain normal invocation",
			argv0:    "brain",
			args:     []string{"server"},
			expected: []string{"server"},
		},
		{
			name:     "brain with project",
			argv0:    "brain",
			args:     []string{"myproject"},
			expected: []string{"myproject"},
		},
		{
			name:     "unknown binary name passthrough",
			argv0:    "unknown-binary",
			args:     []string{"arg1"},
			expected: []string{"arg1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redirectLegacyInvocation(tt.argv0, tt.args)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("redirectLegacyInvocation(%q, %v) = %v, want %v",
					tt.argv0, tt.args, result, tt.expected)
			}
		})
	}
}
