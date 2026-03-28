package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSessionSelectModal_Interface verifies SessionSelectModal implements Modal.
func TestSessionSelectModal_Interface(t *testing.T) {
	var _ Modal = (*SessionSelectModal)(nil)
}

func TestNewSessionSelectModal(t *testing.T) {
	sessionIDs := []string{"ses_abc123", "ses_def456", "ses_ghi789"}
	onSelect := func(id string) tea.Msg { return nil }

	modal := NewSessionSelectModal(sessionIDs, false, onSelect)

	if modal == nil {
		t.Fatal("NewSessionSelectModal returned nil")
	}

	if len(modal.sessionIDs) != 3 {
		t.Errorf("expected 3 session IDs, got %d", len(modal.sessionIDs))
	}

	if modal.selectedIndex != 0 {
		t.Errorf("expected selectedIndex 0, got %d", modal.selectedIndex)
	}

	if modal.tmuxMode != false {
		t.Error("expected tmuxMode false")
	}

	if modal.onSelect == nil {
		t.Error("expected onSelect to be set")
	}
}

func TestNewSessionSelectModal_TmuxMode(t *testing.T) {
	sessionIDs := []string{"ses_abc123"}
	modal := NewSessionSelectModal(sessionIDs, true, func(id string) tea.Msg { return nil })

	if !modal.tmuxMode {
		t.Error("expected tmuxMode true")
	}
}

func TestSessionSelectModal_Title(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_abc"}, false, func(id string) tea.Msg { return nil })

	if got := modal.Title(); got != "Select Session" {
		t.Errorf("Title() = %q, want %q", got, "Select Session")
	}
}

func TestSessionSelectModal_Init(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_abc"}, false, func(id string) tea.Msg { return nil })

	cmd := modal.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestSessionSelectModal_Update(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_abc"}, false, func(id string) tea.Msg { return nil })

	newModal, cmd := modal.Update(nil)
	if newModal == nil {
		t.Error("Update() should return non-nil modal")
	}
	if cmd != nil {
		t.Error("Update() should return nil command")
	}
}

func TestSessionSelectModal_Width(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_abc"}, false, func(id string) tea.Msg { return nil })

	if got := modal.Width(); got != 40 {
		t.Errorf("Width() = %d, want 40", got)
	}
}

func TestSessionSelectModal_Height(t *testing.T) {
	tests := []struct {
		name       string
		sessions   int
		wantHeight int
	}{
		{
			name:       "1 session",
			sessions:   1,
			wantHeight: 1 + 4, // min(1,8) + 4
		},
		{
			name:       "5 sessions",
			sessions:   5,
			wantHeight: 5 + 4, // min(5,8) + 4
		},
		{
			name:       "8 sessions",
			sessions:   8,
			wantHeight: 8 + 4, // min(8,8) + 4
		},
		{
			name:       "10 sessions capped at 8",
			sessions:   10,
			wantHeight: 8 + 4, // min(10,8) + 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := make([]string, tt.sessions)
			for i := range ids {
				ids[i] = "ses_test"
			}
			modal := NewSessionSelectModal(ids, false, func(id string) tea.Msg { return nil })

			if got := modal.Height(); got != tt.wantHeight {
				t.Errorf("Height() = %d, want %d", got, tt.wantHeight)
			}
		})
	}
}

func TestSessionSelectModal_HandleKey_NavigateDown(t *testing.T) {
	sessionIDs := []string{"ses_a", "ses_b", "ses_c"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	// Initially at 0
	if modal.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex 0, got %d", modal.selectedIndex)
	}

	// Press j to move down
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("HandleKey('j') should return handled=true")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("after 'j', selectedIndex = %d, want 1", modal.selectedIndex)
	}

	// Press down to move down again
	handled, _ = modal.HandleKey("down")
	if !handled {
		t.Error("HandleKey('down') should return handled=true")
	}
	if modal.selectedIndex != 2 {
		t.Errorf("after 'down', selectedIndex = %d, want 2", modal.selectedIndex)
	}
}

func TestSessionSelectModal_HandleKey_NavigateUp(t *testing.T) {
	sessionIDs := []string{"ses_a", "ses_b", "ses_c"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	// Move to index 2 first
	modal.selectedIndex = 2

	// Press k to move up
	handled, _ := modal.HandleKey("k")
	if !handled {
		t.Error("HandleKey('k') should return handled=true")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("after 'k', selectedIndex = %d, want 1", modal.selectedIndex)
	}

	// Press up to move up again
	handled, _ = modal.HandleKey("up")
	if !handled {
		t.Error("HandleKey('up') should return handled=true")
	}
	if modal.selectedIndex != 0 {
		t.Errorf("after 'up', selectedIndex = %d, want 0", modal.selectedIndex)
	}
}

func TestSessionSelectModal_HandleKey_WrapDown(t *testing.T) {
	sessionIDs := []string{"ses_a", "ses_b", "ses_c"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	// Move to last item
	modal.selectedIndex = 2

	// Press j should wrap to 0
	modal.HandleKey("j")
	if modal.selectedIndex != 0 {
		t.Errorf("wrapping down: selectedIndex = %d, want 0", modal.selectedIndex)
	}
}

func TestSessionSelectModal_HandleKey_WrapUp(t *testing.T) {
	sessionIDs := []string{"ses_a", "ses_b", "ses_c"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	// At index 0, press k should wrap to last
	modal.HandleKey("k")
	if modal.selectedIndex != 2 {
		t.Errorf("wrapping up: selectedIndex = %d, want 2", modal.selectedIndex)
	}
}

func TestSessionSelectModal_HandleKey_Enter(t *testing.T) {
	sessionIDs := []string{"ses_first", "ses_second", "ses_third"}
	var selectedID string
	onSelect := func(id string) tea.Msg {
		selectedID = id
		return nil
	}

	modal := NewSessionSelectModal(sessionIDs, false, onSelect)
	modal.selectedIndex = 1

	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("HandleKey('enter') should return handled=true")
	}
	if cmd == nil {
		t.Error("HandleKey('enter') should return a command")
	}

	// Execute the command to trigger onSelect
	if cmd != nil {
		cmd()
	}
	if selectedID != "ses_second" {
		t.Errorf("onSelect called with %q, want %q", selectedID, "ses_second")
	}
}

func TestSessionSelectModal_HandleKey_Esc(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_a"}, false, func(id string) tea.Msg { return nil })

	handled, cmd := modal.HandleKey("esc")
	// Returns false so ModalManager can close the modal
	if handled {
		t.Error("HandleKey('esc') should return handled=false so ModalManager closes")
	}
	if cmd != nil {
		t.Error("HandleKey('esc') should return nil command (ModalManager handles close)")
	}
}

func TestSessionSelectModal_HandleKey_ConsumesUnknownKeys(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_a"}, false, func(id string) tea.Msg { return nil })

	handled, cmd := modal.HandleKey("x")
	if !handled {
		t.Error("Unknown keys should be consumed (handled=true)")
	}
	if cmd != nil {
		t.Error("Unknown keys should return nil command")
	}
}

func TestSessionSelectModal_View_ShowsSessions(t *testing.T) {
	sessionIDs := []string{"ses_abc123", "ses_def456", "ses_ghi789"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	// Should contain the header with count
	if !strings.Contains(view, "3 sessions") {
		t.Errorf("View() should contain '3 sessions'\nGot:\n%s", view)
	}

	// Should contain all session IDs
	for _, id := range sessionIDs {
		if !strings.Contains(view, id) {
			t.Errorf("View() should contain session ID %q\nGot:\n%s", id, view)
		}
	}
}

func TestSessionSelectModal_View_ShowsLatestLabel(t *testing.T) {
	sessionIDs := []string{"ses_first", "ses_second"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	// First session should have "(latest)" label
	if !strings.Contains(view, "(latest)") {
		t.Errorf("View() should contain '(latest)' for first session\nGot:\n%s", view)
	}
}

func TestSessionSelectModal_View_SelectedIndicator(t *testing.T) {
	sessionIDs := []string{"ses_a", "ses_b"}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })
	modal.selectedIndex = 0

	view := modal.View()

	// Selected item should have ">" indicator
	if !strings.Contains(view, ">") {
		t.Errorf("View() should contain '>' for selected item\nGot:\n%s", view)
	}
}

func TestSessionSelectModal_View_TruncatesAt8(t *testing.T) {
	// Create 10 sessions
	sessionIDs := make([]string, 10)
	for i := range sessionIDs {
		sessionIDs[i] = "ses_test_" + string(rune('a'+i))
	}
	modal := NewSessionSelectModal(sessionIDs, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	// Should show "and N more" message
	if !strings.Contains(view, "2 more") {
		t.Errorf("View() should contain '2 more' for overflow\nGot:\n%s", view)
	}
}

func TestSessionSelectModal_View_TruncatesLongIDs(t *testing.T) {
	longID := "ses_this_is_a_very_long_session_id_that_exceeds_thirty_characters"
	modal := NewSessionSelectModal([]string{longID}, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	// The full long ID should NOT appear (it's truncated)
	if strings.Contains(view, longID) {
		t.Errorf("View() should truncate long session IDs\nGot:\n%s", view)
	}

	// But a truncated version should appear (first 30 chars + "...")
	truncated := longID[:30]
	if !strings.Contains(view, truncated) {
		t.Errorf("View() should contain truncated ID starting with %q\nGot:\n%s", truncated, view)
	}
}

func TestSessionSelectModal_View_Footer(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_a"}, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	if !strings.Contains(view, "Navigate") {
		t.Errorf("View() should contain navigation hint\nGot:\n%s", view)
	}
	if !strings.Contains(view, "Enter") {
		t.Errorf("View() should contain Enter hint\nGot:\n%s", view)
	}
	if !strings.Contains(view, "Esc") {
		t.Errorf("View() should contain Esc hint\nGot:\n%s", view)
	}
}

func TestSessionSelectModal_View_SingleSession(t *testing.T) {
	modal := NewSessionSelectModal([]string{"ses_only"}, false, func(id string) tea.Msg { return nil })

	view := modal.View()

	if !strings.Contains(view, "1 session") {
		t.Errorf("View() should say '1 session' (singular)\nGot:\n%s", view)
	}
}
