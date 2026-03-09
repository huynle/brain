package tui

import (
	"strings"
	"testing"
)

func TestHelpModal_View(t *testing.T) {
	tests := []struct {
		name           string
		isMultiProject bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:           "single project mode",
			isMultiProject: false,
			wantContains: []string{
				"Navigation:",
				"j/k",
				"Move selection up/down",
				"Actions:",
				"Pause/resume project",
				"Pause/resume all projects",
				"Multi-Select:",
				"Views:",
				"Other:",
				"?",
				"Show this help",
				"Press ? or Esc to close",
			},
			wantNotContain: []string{
				"Projects (Multi-Project Mode):",
				"Previous/next project",
			},
		},
		{
			name:           "multi-project mode",
			isMultiProject: true,
			wantContains: []string{
				"Navigation:",
				"Actions:",
				"Multi-Select:",
				"Views:",
				"Projects (Multi-Project Mode):",
				"h/l",
				"Previous/next project",
				"1-9",
				"Jump to project tab",
				"Other:",
			},
			wantNotContain: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal := NewHelpModal(tt.isMultiProject)
			view := modal.View()

			// Check for expected content
			for _, want := range tt.wantContains {
				if !strings.Contains(view, want) {
					t.Errorf("View() missing expected content %q\nGot:\n%s", want, view)
				}
			}

			// Check for unexpected content
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(view, notWant) {
					t.Errorf("View() contains unexpected content %q\nGot:\n%s", notWant, view)
				}
			}
		})
	}
}

func TestHelpModal_Title(t *testing.T) {
	modal := NewHelpModal(false)
	want := "Keyboard Shortcuts"
	if got := modal.Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestHelpModal_Dimensions(t *testing.T) {
	tests := []struct {
		name           string
		isMultiProject bool
		wantWidth      int
		wantMinHeight  int
	}{
		{
			name:           "single project",
			isMultiProject: false,
			wantWidth:      60,
			wantMinHeight:  27,
		},
		{
			name:           "multi-project",
			isMultiProject: true,
			wantWidth:      60,
			wantMinHeight:  30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal := NewHelpModal(tt.isMultiProject)

			if got := modal.Width(); got != tt.wantWidth {
				t.Errorf("Width() = %d, want %d", got, tt.wantWidth)
			}

			if got := modal.Height(); got < tt.wantMinHeight {
				t.Errorf("Height() = %d, want at least %d", got, tt.wantMinHeight)
			}
		})
	}
}

func TestHelpModal_HandleKey(t *testing.T) {
	modal := NewHelpModal(false)

	tests := []struct {
		name        string
		key         string
		wantHandled bool
	}{
		{
			name:        "? closes modal",
			key:         "?",
			wantHandled: true,
		},
		{
			name:        "q closes modal",
			key:         "q",
			wantHandled: true,
		},
		{
			name:        "other keys consumed",
			key:         "j",
			wantHandled: true,
		},
		{
			name:        "esc consumed",
			key:         "esc",
			wantHandled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled, _ := modal.HandleKey(tt.key)
			if handled != tt.wantHandled {
				t.Errorf("HandleKey(%q) handled = %v, want %v", tt.key, handled, tt.wantHandled)
			}
		})
	}
}

func TestHelpModal_View_ContainsPauseShortcuts(t *testing.T) {
	modal := NewHelpModal(false)
	view := modal.View()

	// Should contain pause shortcuts in Actions section
	if !strings.Contains(view, "Pause/resume project") {
		t.Errorf("View() missing 'Pause/resume project' shortcut\nGot:\n%s", view)
	}
	if !strings.Contains(view, "Pause/resume all projects") {
		t.Errorf("View() missing 'Pause/resume all projects' shortcut\nGot:\n%s", view)
	}
}

func TestHelpModal_Height_IncludesPauseLines(t *testing.T) {
	// Height should account for the 2 new pause shortcut lines
	modal := NewHelpModal(false)
	height := modal.Height()

	// baseLines = 5+9+3+4+2+5+2 = 30
	if height < 27 {
		t.Errorf("Height() = %d, expected at least 27 (includes pause shortcut lines)", height)
	}
}

func TestHelpModal_Init(t *testing.T) {
	modal := NewHelpModal(false)
	cmd := modal.Init()
	if cmd != nil {
		t.Errorf("Init() returned non-nil command, expected nil")
	}
}

func TestHelpModal_Update(t *testing.T) {
	modal := NewHelpModal(false)

	// Test that Update returns the same modal and no command
	newModal, cmd := modal.Update(nil)
	if newModal != modal {
		t.Errorf("Update() returned different modal instance")
	}
	if cmd != nil {
		t.Errorf("Update() returned non-nil command, expected nil")
	}
}
