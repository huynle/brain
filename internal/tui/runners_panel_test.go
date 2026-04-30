package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func TestRunnersPanel_RenderRunnerRow_ShowsSelectionMarkers(t *testing.T) {
	panel := NewRunnersPanel()
	runner := types.RunnerInfo{
		RunnerID:      "runner-1",
		Hostname:      "host-1",
		Status:        "online",
		MaxParallel:   2,
		LastHeartbeat: time.Now().UTC().Format(time.RFC3339),
	}

	unselected := panel.renderRunnerRow(runner, false, 100)
	if !strings.Contains(unselected, "[ ]") {
		t.Fatalf("expected unselected runner row to include checkbox marker, got %q", unselected)
	}

	selected := panel.renderRunnerRow(runner, true, 100)
	if !strings.Contains(selected, "[x]") {
		t.Fatalf("expected selected runner row to include checked marker, got %q", selected)
	}
}
