package tui

import (
	"testing"
)

// =============================================================================
// StatusStyleWithState Tests
// =============================================================================

func TestStatusStyleWithState_InProgressStatus(t *testing.T) {
	// Status should take precedence over classification
	style := StatusStyleWithState("in_progress", "ready")
	if style.GetForeground() != ColorActive {
		t.Errorf("expected ColorActive for in_progress status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_CompletedStatus(t *testing.T) {
	style := StatusStyleWithState("completed", "ready")
	if style.GetForeground() != ColorInactiveCompleted {
		t.Errorf("expected ColorInactiveCompleted for completed status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_CancelledStatus(t *testing.T) {
	style := StatusStyleWithState("cancelled", "ready")
	if style.GetForeground() != ColorInactiveCancelled {
		t.Errorf("expected ColorInactiveCancelled for cancelled status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_ValidatedStatus(t *testing.T) {
	style := StatusStyleWithState("validated", "ready")
	if style.GetForeground() != ColorInactiveValidated {
		t.Errorf("expected ColorInactiveValidated for validated status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_SupersededStatus(t *testing.T) {
	style := StatusStyleWithState("superseded", "ready")
	if style.GetForeground() != ColorInactiveSuperseded {
		t.Errorf("expected ColorInactiveSuperseded for superseded status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_ArchivedStatus(t *testing.T) {
	style := StatusStyleWithState("archived", "ready")
	if style.GetForeground() != ColorInactiveArchived {
		t.Errorf("expected ColorInactiveArchived for archived status, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_ReadyClassification(t *testing.T) {
	// When status is pending, should fall back to classification
	style := StatusStyleWithState("pending", "ready")
	if style.GetForeground() != ColorReady {
		t.Errorf("expected ColorReady for ready classification, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_WaitingClassification(t *testing.T) {
	style := StatusStyleWithState("pending", "waiting")
	if style.GetForeground() != ColorWaiting {
		t.Errorf("expected ColorWaiting for waiting classification, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_BlockedClassification(t *testing.T) {
	style := StatusStyleWithState("pending", "blocked")
	if style.GetForeground() != ColorBlocked {
		t.Errorf("expected ColorBlocked for blocked classification, got %v", style.GetForeground())
	}
}

func TestStatusStyleWithState_DefaultToCompleted(t *testing.T) {
	// Unknown classification should default to waiting
	style := StatusStyleWithState("pending", "unknown")
	if style.GetForeground() != ColorWaiting {
		t.Errorf("expected ColorWaiting for unknown classification, got %v", style.GetForeground())
	}
}
