package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestSaveUpdateRecall_AttachmentsRoundTrip(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "report",
		Title:   "Attachment Report",
		Content: "Body",
		Attachments: []types.AttachmentReference{{
			ID:          "att_x",
			Filename:    "notes.pdf",
			ContentType: "application/pdf",
			Size:        1234,
			Role:        "source",
		}},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updatedAttachments := []types.AttachmentReference{{
		ID:      "att_y",
		Caption: "Updated attachment",
	}}
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{Attachments: &updatedAttachments})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if len(updated.Attachments) != 1 || updated.Attachments[0].ID != "att_y" || updated.Attachments[0].Caption != "Updated attachment" {
		t.Fatalf("updated attachments = %#v, want att_y", updated.Attachments)
	}

	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if len(recalled.Attachments) != 1 || recalled.Attachments[0].ID != "att_y" {
		t.Fatalf("recalled attachments = %#v, want att_y", recalled.Attachments)
	}

	content, err := os.ReadFile(filepath.Join(brainDir, filepath.FromSlash(saved.Path)))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	text := string(content)
	for _, want := range []string{"attachments:", "  - id: att_y", "    caption: Updated attachment"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved file missing %q in:\n%s", want, text)
		}
	}
}
