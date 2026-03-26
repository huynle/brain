package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// sessionOpenedMsg is sent when a session has been opened (or failed to open).
type sessionOpenedMsg struct {
	taskID    string
	sessionID string
	err       error
}

// sessionsFetchedMsg is sent when sessions have been fetched from the API.
type sessionsFetchedMsg struct {
	sessionIDs []string
	taskPath   string
	tmuxMode   bool
	err        error
}

// sessionSelectedMsg is sent when a user selects a session from the modal.
type sessionSelectedMsg struct {
	sessionID string
	tmuxMode  bool
	taskID    string
}

// sortedSessionIDs extracts session IDs from a Sessions map, sorted by
// timestamp descending (most recent first).
func sortedSessionIDs(sessions map[string]types.SessionInfo) []string {
	if len(sessions) == 0 {
		return nil
	}

	type entry struct {
		id        string
		timestamp string
	}
	entries := make([]entry, 0, len(sessions))
	for id, info := range sessions {
		entries = append(entries, entry{id: id, timestamp: info.Timestamp})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].timestamp > entries[j].timestamp // descending
	})

	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	return ids
}

// extractTaskID extracts the task ID from a task path.
// e.g. "projects/test/task/abc12def.md" → "abc12def"
func extractTaskID(taskPath string) string {
	base := filepath.Base(taskPath)
	return strings.TrimSuffix(base, ".md")
}

// getOpencodeBin returns the opencode binary path from env or default.
func getOpencodeBin() string {
	bin := os.Getenv("OPENCODE_BIN")
	if bin == "" {
		bin = "opencode"
	}
	return bin
}

// openSessionFullscreen opens an OpenCode session in fullscreen mode,
// taking over the terminal (like $EDITOR).
func openSessionFullscreen(sessionID string, taskID string) tea.Cmd {
	opencodeBin := getOpencodeBin()
	cmd := exec.Command(opencodeBin, "-s", sessionID)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return sessionOpenedMsg{taskID: taskID, sessionID: sessionID, err: err}
	})
}

// openSessionTmux opens an OpenCode session in a new tmux window.
func openSessionTmux(sessionID string, taskID string) tea.Cmd {
	return func() tea.Msg {
		opencodeBin := getOpencodeBin()

		// Build window name from sessionID (truncated for tmux)
		windowName := sessionID
		if len(windowName) > 20 {
			windowName = windowName[:20]
		}

		cmd := exec.Command("tmux", "new-window", "-n", windowName, opencodeBin, "-s", sessionID)
		err := cmd.Run()
		return sessionOpenedMsg{taskID: taskID, sessionID: sessionID, err: err}
	}
}

// fetchSessionsCmd fetches sessions for a task from the Brain API.
func fetchSessionsCmd(apiClient *runner.APIClient, taskPath string, tmuxMode bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		entry, err := apiClient.GetEntry(ctx, taskPath)
		if err != nil {
			return sessionsFetchedMsg{err: err, taskPath: taskPath, tmuxMode: tmuxMode}
		}

		if entry == nil || len(entry.Sessions) == 0 {
			return sessionsFetchedMsg{
				sessionIDs: nil,
				taskPath:   taskPath,
				tmuxMode:   tmuxMode,
				err:        fmt.Errorf("no sessions found for task"),
			}
		}

		// Extract session IDs and sort by timestamp descending (latest first)
		type sessionEntry struct {
			id        string
			timestamp string
		}
		entries := make([]sessionEntry, 0, len(entry.Sessions))
		for id, info := range entry.Sessions {
			entries = append(entries, sessionEntry{id: id, timestamp: info.Timestamp})
		}
		sort.Slice(entries, func(i, j int) bool {
			// Descending: later timestamps first
			return entries[i].timestamp > entries[j].timestamp
		})

		sessionIDs := make([]string, len(entries))
		for i, e := range entries {
			sessionIDs[i] = e.id
		}

		return sessionsFetchedMsg{
			sessionIDs: sessionIDs,
			taskPath:   taskPath,
			tmuxMode:   tmuxMode,
		}
	}
}
