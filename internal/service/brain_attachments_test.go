package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/storage"
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

func TestRecallWithIncludeAttachmentsHydratesMetadataOnly(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	row, err := store.CreateAttachment(ctx, storage.AttachmentInput{
		Digest:    strings.Repeat("a", 64),
		Size:      1234,
		MediaType: "application/pdf",
		Metadata:  `{"filename":"source.pdf","project_id":"default","purpose":"spec"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "report",
		Title:   "Metadata-only Attachment Recall",
		Content: "Body",
		Attachments: []types.AttachmentReference{{
			ID:      "1",
			Role:    "source",
			Caption: "original caption",
			Derived: []types.AttachmentDerived{{
				ID:          "derived-text",
				Kind:        "text",
				ContentType: "text/plain",
				Size:        42,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, saved.Path, row.ID, "source"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{
		AttachmentID: row.ID,
		Kind:         "text",
		Status:       types.AttachmentExtractionStatusReady,
		ContentType:  "text/markdown",
		Text:         "ready derived text",
		Metadata:     `{"provider":"openrouter","model":"google/gemini-2.5-flash"}`,
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived failed: %v", err)
	}

	defaultRecall, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall without include failed: %v", err)
	}
	if got := defaultRecall.Attachments[0]; got.Filename != "" || got.DownloadURL != "" || got.TextURL != "" || got.Metadata != nil {
		t.Fatalf("default attachment ref = %#v, want no hydrated metadata", got)
	}

	included, err := svc.Recall(ctx, saved.ID, "attachments", "derived")
	if err != nil {
		t.Fatalf("Recall with include failed: %v", err)
	}
	if len(included.Attachments) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(included.Attachments))
	}
	got := included.Attachments[0]
	if got.ID != "1" || got.Filename != "source.pdf" || got.ContentType != "application/pdf" || got.Size != 1234 || got.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("hydrated attachment = %#v, want stored metadata", got)
	}
	if got.Role != "source" || got.Caption != "original caption" || len(got.Derived) != 1 || got.Derived[0].ID != "derived-text" {
		t.Fatalf("hydrated attachment = %#v, want frontmatter role/caption/derived preserved", got)
	}
	if got.Metadata["purpose"] != "spec" || got.Metadata["project_id"] != "default" {
		t.Fatalf("metadata = %#v, want stored metadata", got.Metadata)
	}
	if got.DownloadURL != "/api/v1/attachments/1/content?project_id=default" {
		t.Fatalf("DownloadURL = %q", got.DownloadURL)
	}
	if got.TextURL != "/api/v1/attachments/1/text?project_id=default" {
		t.Fatalf("TextURL = %q", got.TextURL)
	}
	if got.DerivedText == nil || got.DerivedText.Status != types.AttachmentExtractionStatusReady || got.DerivedText.Metadata["model"] != "google/gemini-2.5-flash" {
		t.Fatalf("DerivedText = %#v, want ready status and model metadata", got.DerivedText)
	}
}

func TestListAndSearchIncludeAttachmentsHydratesMetadataOnly(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	row, err := store.CreateAttachment(ctx, storage.AttachmentInput{
		Digest:    strings.Repeat("b", 64),
		Size:      99,
		MediaType: "text/plain",
		Metadata:  `{"filename":"notes.txt","project_id":"default"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "report",
		Title:   "Searchable Attachment Metadata Entry",
		Content: "unique phase two body",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, saved.Path, row.ID, "source"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}

	listedDefault, err := svc.List(ctx, types.ListEntriesRequest{Filename: saved.ID})
	if err != nil {
		t.Fatalf("List without include failed: %v", err)
	}
	if len(listedDefault.Entries) != 1 {
		t.Fatalf("default list entries len = %d, want 1", len(listedDefault.Entries))
	}
	if len(listedDefault.Entries[0].Attachments) != 0 {
		t.Fatalf("default list attachments = %#v, want none", listedDefault.Entries[0].Attachments)
	}

	listed, err := svc.List(ctx, types.ListEntriesRequest{Filename: saved.ID, Include: []string{"attachments"}})
	if err != nil {
		t.Fatalf("List with include failed: %v", err)
	}
	if got := listed.Entries[0].Attachments; len(got) != 1 || got[0].Filename != "notes.txt" || got[0].DownloadURL == "" || got[0].TextURL == "" {
		t.Fatalf("list attachments = %#v, want hydrated metadata URLs", got)
	}

	searchDefault, err := svc.Search(ctx, types.SearchRequest{Query: "unique", Limit: intPtr(10)})
	if err != nil {
		t.Fatalf("Search without include failed: %v", err)
	}
	if len(searchDefault.Results) != 1 {
		t.Fatalf("default search results len = %d, want 1", len(searchDefault.Results))
	}
	if len(searchDefault.Results[0].Attachments) != 0 {
		t.Fatalf("default search attachments = %#v, want none", searchDefault.Results[0].Attachments)
	}

	search, err := svc.Search(ctx, types.SearchRequest{Query: "unique", Include: []string{"attachments", "derived"}, Limit: intPtr(10)})
	if err != nil {
		t.Fatalf("Search with include failed: %v", err)
	}
	if got := search.Results[0].Attachments; len(got) != 1 || got[0].Filename != "notes.txt" || got[0].DownloadURL == "" || got[0].TextURL == "" {
		t.Fatalf("search attachments = %#v, want hydrated metadata URLs", got)
	}
}

func TestSearchAttachmentDerivedTextSetsMatchSource(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	row, err := store.CreateAttachment(ctx, storage.AttachmentInput{
		Digest:    strings.Repeat("c", 64),
		Size:      321,
		MediaType: "application/pdf",
		Metadata:  `{"filename":"derived.pdf","project_id":"default"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "report",
		Title:   "Attachment Derived Text Search",
		Content: "body without searchable derived token",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, saved.Path, row.ID, "source"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{
		AttachmentID: row.ID,
		Kind:         "text",
		Status:       "ready",
		ContentType:  "text/plain",
		Text:         "servicederivedneedle exists only in extracted attachment text",
		Metadata:     `{}`,
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived failed: %v", err)
	}

	search, err := svc.Search(ctx, types.SearchRequest{Query: "servicederivedneedle", Include: []string{"attachments"}, Limit: intPtr(10)})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(search.Results) != 1 {
		t.Fatalf("search results len = %d, want 1", len(search.Results))
	}
	if search.Results[0].ID != saved.ID {
		t.Fatalf("search result ID = %q, want %q", search.Results[0].ID, saved.ID)
	}
	if search.Results[0].MatchSource != "attachment" {
		t.Fatalf("MatchSource = %q, want attachment", search.Results[0].MatchSource)
	}
	if got := search.Results[0].Attachments; len(got) != 1 || got[0].Filename != "derived.pdf" || got[0].DownloadURL == "" {
		t.Fatalf("search attachments = %#v, want hydrated attachment metadata", got)
	}
}

func TestAttachmentEndToEndSafetyAndDerivedSearch(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	blobRoot := t.TempDir()
	blobs, err := blobstore.NewFilesystemStore(blobRoot, 32)
	if err != nil {
		t.Fatalf("NewFilesystemStore failed: %v", err)
	}
	attachments := NewAttachmentService(store, blobs, brain, 32)
	ctx := context.Background()

	entry, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "report",
		Title:   "End-to-End Attachment Entry",
		Content: "entry body without derived search token",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	content := []byte("hello derived attachment")
	sum := sha256.Sum256(content)
	wantSHA := hex.EncodeToString(sum[:])
	created, err := attachments.Create(ctx, "default", types.CreateAttachmentRequest{
		Filename:    "source.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
		SHA256:      wantSHA,
		Metadata:    map[string]string{"purpose": "e2e"},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if created.Attachment.SHA256 != wantSHA {
		t.Fatalf("created SHA256 = %q, want %q", created.Attachment.SHA256, wantSHA)
	}

	attached, err := attachments.Attach(ctx, "default", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: created.Attachment.ID, Role: "source", Caption: "fixture"}})
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}
	if len(attached.Attachments) != 1 || attached.Attachments[0].ID != created.Attachment.ID {
		t.Fatalf("Attach response = %#v", attached)
	}

	recalled, err := brain.Recall(ctx, entry.ID, "attachments")
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if got := recalled.Attachments; len(got) != 1 || got[0].Filename != "source.txt" || got[0].SHA256 != wantSHA || got[0].DownloadURL == "" {
		t.Fatalf("recalled attachments = %#v, want hydrated metadata and download URL", got)
	}

	_, stream, err := attachments.Open(ctx, "default", created.Attachment.ID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	downLoaded, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	downloadSum := sha256.Sum256(downLoaded)
	if gotSHA := hex.EncodeToString(downloadSum[:]); gotSHA != wantSHA {
		t.Fatalf("download SHA256 = %q, want %q", gotSHA, wantSHA)
	}

	if _, err := attachments.StoreDerivedText(ctx, "default", created.Attachment.ID, types.AttachmentDerivedText{Kind: "text", Status: "ready", ContentType: "text/plain", Text: "uniquephase3derivedneedle"}); err != nil {
		t.Fatalf("StoreDerivedText failed: %v", err)
	}
	search, err := brain.Search(ctx, types.SearchRequest{Query: "uniquephase3derivedneedle", Include: []string{"attachments"}, Limit: intPtr(10)})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(search.Results) != 1 || search.Results[0].ID != entry.ID || search.Results[0].MatchSource != "attachment" {
		t.Fatalf("derived search = %#v, want entry match from attachment", search.Results)
	}

	deleted, err := attachments.Delete(ctx, "default", created.Attachment.ID)
	if err != nil {
		t.Fatalf("Delete referenced failed: %v", err)
	}
	if deleted {
		t.Fatal("Delete removed referenced attachment; want refusal")
	}

	if _, err := attachments.Detach(ctx, "default", entry.ID, created.Attachment.ID, "source"); err != nil {
		t.Fatalf("Detach failed: %v", err)
	}
	deleted, err = attachments.Delete(ctx, "default", created.Attachment.ID)
	if err != nil {
		t.Fatalf("Delete detached failed: %v", err)
	}
	if !deleted {
		t.Fatal("Delete detached returned false, want true")
	}
	if _, _, err := attachments.Open(ctx, "default", created.Attachment.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Open deleted err = %v, want api.ErrNotFound", err)
	}

	if _, err := attachments.Create(ctx, "default", types.CreateAttachmentRequest{Filename: "too-large.txt", Size: 33}, strings.NewReader(strings.Repeat("x", 33))); !errors.Is(err, blobstore.ErrTooLarge) {
		t.Fatalf("oversized create err = %v, want ErrTooLarge", err)
	}
	if _, err := attachments.Create(ctx, "default", types.CreateAttachmentRequest{Filename: "../unsafe.txt", Size: 4}, strings.NewReader("safe")); err == nil || !strings.Contains(err.Error(), "filename") {
		t.Fatalf("unsafe filename err = %v, want filename validation", err)
	}
}

func TestStoreDerivedTextRefreshesLinkedEntryEmbeddings(t *testing.T) {
	var embeddedInputs []string
	brain, store, _ := newTestBrainServiceWithEmbedding(t, &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			embeddedInputs = append([]string(nil), inputs...)
			vectors := make([][]float32, len(inputs))
			for i := range inputs {
				vectors[i] = []float32{0.1, 0.2, 0.3, 0.4, 0.5}
			}
			return vectors, nil
		},
	})
	blobs := newRecordingBlobStore()
	attachments := NewAttachmentService(store, blobs, brain, 1024, WithAttachmentDerivedChangeHook(brain))
	ctx := context.Background()

	entry, err := brain.Save(ctx, types.CreateEntryRequest{Type: "report", Title: "Embeddable Attachment Entry", Content: "entry body", Project: "default"})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	content := []byte("image bytes")
	created, err := attachments.Create(ctx, "default", types.CreateAttachmentRequest{Filename: "scan.png", ContentType: "image/png", Size: int64(len(content))}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := attachments.Attach(ctx, "default", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: created.Attachment.ID, Role: "source"}}); err != nil {
		t.Fatalf("Attach failed: %v", err)
	}
	embeddedInputs = nil

	if _, err := attachments.StoreDerivedText(ctx, "default", created.Attachment.ID, types.AttachmentDerivedText{Kind: "text", Status: "ready", ContentType: "text/plain", Text: "fresh semantic derived attachment text"}); err != nil {
		t.Fatalf("StoreDerivedText failed: %v", err)
	}

	foundDerivedText := false
	for _, input := range embeddedInputs {
		if strings.Contains(input, "fresh semantic derived attachment text") {
			foundDerivedText = true
			break
		}
	}
	if !foundDerivedText {
		t.Fatalf("embedding inputs = %#v, want refreshed linked entry embedding to include derived attachment text", embeddedInputs)
	}
}
