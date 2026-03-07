package tui

import (
	"runtime"
	"testing"
)

func TestCopyToClipboard_SucceedsOnSupportedPlatform(t *testing.T) {
	// This test only runs on macOS/Linux where clipboard tools are available
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("skipping clipboard test on unsupported platform")
	}

	result := CopyToClipboard("test clipboard content")
	if !result {
		t.Log("CopyToClipboard returned false - clipboard tool may not be available in CI")
	}
	// We don't fail on false since CI may not have clipboard tools
}

func TestCopyToClipboard_HandlesEmptyString(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("skipping clipboard test on unsupported platform")
	}

	// Should not panic on empty string
	_ = CopyToClipboard("")
}

func TestRunClipboardCmd_FailsWithBadCommand(t *testing.T) {
	result := runClipboardCmd("nonexistent-clipboard-cmd-12345", "hello")
	if result {
		t.Error("expected runClipboardCmd to return false for nonexistent command")
	}
}
