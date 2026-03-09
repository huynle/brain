package tui

import (
	"os/exec"
	"runtime"
	"strings"
)

// CopyToClipboard copies the given text to the system clipboard.
// Uses platform-specific commands:
//   - macOS: pbcopy
//   - Linux: xclip (falls back to xsel)
//   - Windows: clip
//
// Returns true if successful, false otherwise.
func CopyToClipboard(text string) bool {
	switch runtime.GOOS {
	case "darwin":
		return runClipboardCmd("pbcopy", text)
	case "linux":
		// Try xclip first, then xsel
		if runClipboardCmd("xclip", text, "-selection", "clipboard") {
			return true
		}
		return runClipboardCmd("xsel", text, "--clipboard", "--input")
	case "windows":
		return runClipboardCmd("clip", text)
	default:
		return false
	}
}

// runClipboardCmd runs a clipboard command with the given text as stdin.
func runClipboardCmd(name string, text string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() == nil
}
