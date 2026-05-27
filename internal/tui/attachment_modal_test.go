package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestAttachmentModal_XTriggersExtractForSelectedAttachment(t *testing.T) {
	modal := NewAttachmentModal([]types.AttachmentReference{
		{ID: "att_1", Filename: "one.pdf"},
		{ID: "att_2", Filename: "two.png"},
	}, 1)

	handled, cmd := modal.HandleKey("x")
	if !handled {
		t.Fatal("expected x key to be handled")
	}
	if cmd == nil {
		t.Fatal("expected x key to return an action command")
	}
	msg, ok := cmd().(attachmentModalActionMsg)
	if !ok {
		t.Fatalf("expected attachmentModalActionMsg, got %T", cmd())
	}
	if msg.Action != "extract" {
		t.Fatalf("action = %q, want extract", msg.Action)
	}
	if msg.Attachment.ID != "att_2" {
		t.Fatalf("attachment ID = %q, want att_2", msg.Attachment.ID)
	}
}

func TestAttachmentModal_ViewIncludesExtractActionHint(t *testing.T) {
	modal := NewAttachmentModal([]types.AttachmentReference{{ID: "att_1", Filename: "one.pdf"}}, 0)

	view := modal.View()
	if !strings.Contains(view, "x: extract") {
		t.Fatalf("expected extract action hint, got:\n%s", view)
	}
}

func TestAttachmentModal_ViewRendersExtractionStatusModelAndError(t *testing.T) {
	modal := NewAttachmentModal([]types.AttachmentReference{
		{
			ID:       "att_ready",
			Filename: "ready.png",
			Derived:  []types.AttachmentDerived{{Kind: "text", ContentType: "text/markdown", Size: 128}},
			DerivedText: &types.AttachmentDerivedText{
				Status:   types.AttachmentExtractionStatusReady,
				Metadata: map[string]string{"provider": "openrouter", "model": "google/gemini-2.5-flash"},
			},
		},
		{
			ID:       "att_failed",
			Filename: "failed.pdf",
			DerivedText: &types.AttachmentDerivedText{
				Status: types.AttachmentExtractionStatusFailed,
				Error:  "unsupported PDF encoding",
			},
		},
	}, 0)

	view := modal.View()
	for _, want := range []string{
		"Text: ready",
		"Extraction: ready",
		"Model: openrouter / google/gemini-2.5-flash",
		"Text: none",
		"Extraction: failed",
		"Error: unsupported PDF encoding",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected extraction detail %q, got:\n%s", want, view)
		}
	}
}
