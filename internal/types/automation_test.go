package types

import "testing"

func TestNormalizeAutomationActionType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"canonical prompt", "prompt", AutomationActionPrompt},
		{"canonical script", "script", AutomationActionScript},
		{"canonical update", "update", AutomationActionUpdate},
		{"canonical http", "http", AutomationActionHTTP},
		{"shell alias maps to script", "shell", AutomationActionScript},
		{"shell alias uppercase", "SHELL", AutomationActionScript},
		{"shell alias mixed case", "Shell", AutomationActionScript},
		{"shell alias with whitespace", "  shell  ", AutomationActionScript},
		{"empty stays empty", "", ""},
		{"whitespace-only stays empty", "   ", ""},
		{"unknown value lowercased and trimmed", "  CustomFoo  ", "customfoo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAutomationActionType(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeAutomationActionType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
