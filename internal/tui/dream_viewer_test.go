package tui

import (
	"strings"
	"testing"
)

func TestDreamViewer_InitialState(t *testing.T) {
	dv := NewDreamViewer()

	if dv.loading {
		t.Error("new DreamViewer should not be loading")
	}
	if dv.fetched {
		t.Error("new DreamViewer should not have fetched content")
	}
	if dv.errMsg != "" {
		t.Error("new DreamViewer should have no error")
	}
	if dv.content != "" {
		t.Error("new DreamViewer should have no content")
	}
}

func TestDreamViewer_SetLoading(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetLoading(true)

	if !dv.loading {
		t.Error("DreamViewer should be loading after SetLoading(true)")
	}

	view := dv.View(80, 24)
	if !strings.Contains(view, "Fetching dream") {
		t.Errorf("loading view should contain 'Fetching dream', got: %s", view)
	}
}

func TestDreamViewer_SetContent(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetLoading(true)
	dv.SetContent("# My Dream\n\nThis is the dream content.")

	if dv.loading {
		t.Error("DreamViewer should not be loading after SetContent")
	}
	if !dv.fetched {
		t.Error("DreamViewer should be fetched after SetContent")
	}
	if dv.content != "# My Dream\n\nThis is the dream content." {
		t.Errorf("unexpected content: %s", dv.content)
	}
	if dv.errMsg != "" {
		t.Error("DreamViewer should have no error after SetContent")
	}
}

func TestDreamViewer_SetError(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetLoading(true)
	dv.SetError("connection refused")

	if dv.loading {
		t.Error("DreamViewer should not be loading after SetError")
	}
	if !dv.fetched {
		t.Error("DreamViewer should be fetched after SetError")
	}
	if dv.errMsg != "connection refused" {
		t.Errorf("unexpected error: %s", dv.errMsg)
	}

	view := dv.View(80, 24)
	if !strings.Contains(view, "connection refused") {
		t.Errorf("error view should contain error message, got: %s", view)
	}
}

func TestDreamViewer_EmptyContent(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetContent("")

	view := dv.View(80, 24)
	if !strings.Contains(view, "No dream found") {
		t.Errorf("empty content view should show 'No dream found', got: %s", view)
	}
}

func TestDreamViewer_HasContent(t *testing.T) {
	dv := NewDreamViewer()

	if dv.HasContent() {
		t.Error("new DreamViewer should not have content")
	}

	dv.SetContent("some content")
	if !dv.HasContent() {
		t.Error("DreamViewer should have content after SetContent")
	}
}

func TestDreamViewer_SetContentClearsError(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetError("some error")
	dv.SetContent("new content")

	if dv.errMsg != "" {
		t.Error("SetContent should clear error message")
	}
}

func TestDreamViewer_SetLoadingClearsError(t *testing.T) {
	dv := NewDreamViewer()
	dv.SetError("some error")
	dv.SetLoading(true)

	if dv.errMsg != "" {
		t.Error("SetLoading should clear error message")
	}
}
