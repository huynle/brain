package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

type recordingBlobStore struct {
	blobs      map[string][]byte
	putErr     error
	getErr     error
	deleteErr  error
	putCalls   int
	getCalls   []string
	deleteCall []string
}

type mockBrainForAttachments struct {
	api.BrainService
	entries     map[string]*types.BrainEntry
	updateErr   error
	updates     []types.UpdateEntryRequest
	recallCalls []string
	updateCalls []string
}

type stubAttachmentExtractor struct{}

func (stubAttachmentExtractor) Extract(context.Context, types.AttachmentExtractionRequest) (types.AttachmentExtractionResponse, error) {
	return types.AttachmentExtractionResponse{}, nil
}

type recordingAttachmentExtractor struct {
	resp  types.AttachmentExtractionResponse
	err   error
	calls []types.AttachmentExtractionRequest
}

func (e *recordingAttachmentExtractor) Extract(_ context.Context, req types.AttachmentExtractionRequest) (types.AttachmentExtractionResponse, error) {
	e.calls = append(e.calls, req)
	return e.resp, e.err
}

func newMockBrainForAttachments(entries ...*types.BrainEntry) *mockBrainForAttachments {
	m := &mockBrainForAttachments{entries: map[string]*types.BrainEntry{}}
	for _, entry := range entries {
		m.entries[entry.Path] = cloneBrainEntryForAttachmentTest(entry)
		if entry.ID != "" {
			m.entries[entry.ID] = cloneBrainEntryForAttachmentTest(entry)
		}
	}
	return m
}

func (m *mockBrainForAttachments) Recall(_ context.Context, pathOrID string, include ...string) (*types.BrainEntry, error) {
	m.recallCalls = append(m.recallCalls, pathOrID)
	entry := m.entries[pathOrID]
	if entry == nil {
		return nil, api.ErrNotFound
	}
	return cloneBrainEntryForAttachmentTest(entry), nil
}

func (m *mockBrainForAttachments) Update(_ context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
	m.updateCalls = append(m.updateCalls, pathOrID)
	m.updates = append(m.updates, req)
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	entry := m.entries[pathOrID]
	if entry == nil {
		return nil, api.ErrNotFound
	}
	updated := cloneBrainEntryForAttachmentTest(entry)
	if req.Attachments != nil {
		updated.Attachments = append([]types.AttachmentReference(nil), (*req.Attachments)...)
	}
	m.entries[updated.Path] = cloneBrainEntryForAttachmentTest(updated)
	if updated.ID != "" {
		m.entries[updated.ID] = cloneBrainEntryForAttachmentTest(updated)
	}
	return updated, nil
}

func cloneBrainEntryForAttachmentTest(entry *types.BrainEntry) *types.BrainEntry {
	if entry == nil {
		return nil
	}
	clone := *entry
	clone.Attachments = append([]types.AttachmentReference(nil), entry.Attachments...)
	return &clone
}

func newRecordingBlobStore() *recordingBlobStore {
	return &recordingBlobStore{blobs: map[string][]byte{}}
}

func (b *recordingBlobStore) Put(r io.Reader) (string, int64, error) {
	b.putCalls++
	if b.putErr != nil {
		return "", 0, b.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	b.blobs[digest] = append([]byte(nil), data...)
	return digest, int64(len(data)), nil
}

func (b *recordingBlobStore) Get(hash string) (io.ReadCloser, error) {
	b.getCalls = append(b.getCalls, hash)
	if b.getErr != nil {
		return nil, b.getErr
	}
	data, ok := b.blobs[hash]
	if !ok {
		return nil, blobstore.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *recordingBlobStore) Delete(hash string) error {
	b.deleteCall = append(b.deleteCall, hash)
	if b.deleteErr != nil {
		return b.deleteErr
	}
	if _, ok := b.blobs[hash]; !ok {
		return blobstore.ErrNotFound
	}
	delete(b.blobs, hash)
	return nil
}

func newAttachmentServiceForTest(t *testing.T, maxSize int64) (*AttachmentServiceImpl, *storage.StorageLayer, *recordingBlobStore) {
	t.Helper()
	store, err := storage.New(t.TempDir() + "/brain.db")
	if err != nil {
		t.Fatalf("storage.New failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	blobs := newRecordingBlobStore()
	return NewAttachmentService(store, blobs, nil, maxSize), store, blobs
}

func newAttachmentServiceWithBrainForTest(t *testing.T, brain api.BrainService) (*AttachmentServiceImpl, *storage.StorageLayer, *recordingBlobStore) {
	t.Helper()
	store, err := storage.New(t.TempDir() + "/brain.db")
	if err != nil {
		t.Fatalf("storage.New failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	blobs := newRecordingBlobStore()
	return NewAttachmentService(store, blobs, brain, 1024), store, blobs
}

func createAttachmentForServiceTest(t *testing.T, svc *AttachmentServiceImpl, projectID string, filename string) types.Attachment {
	t.Helper()
	content := strings.NewReader("data")
	created, err := svc.Create(context.Background(), projectID, types.CreateAttachmentRequest{Filename: filename, Size: 4}, content)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	return created.Attachment
}

func insertAttachmentNoteForTest(t *testing.T, store *storage.StorageLayer, projectID, path, shortID string) *types.BrainEntry {
	t.Helper()
	if _, err := store.InsertNote(context.Background(), &storage.NoteRow{Path: path, ShortID: shortID, Title: "Attachment note"}); err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}
	return &types.BrainEntry{ID: shortID, Path: path, Title: "Attachment note", ProjectID: projectID}
}

func TestNewAttachmentServiceImplementsAPIContract(t *testing.T) {
	var _ api.AttachmentService = NewAttachmentService(nil, nil, nil, 0)
}

func TestAttachmentServiceCanInjectExtractor(t *testing.T) {
	extractor := stubAttachmentExtractor{}
	svc := NewAttachmentService(nil, nil, nil, 0, WithAttachmentExtractor(extractor))
	if svc.extractor == nil {
		t.Fatal("WithAttachmentExtractor did not set extractor dependency")
	}
}

func TestAttachmentServiceExtractAttachmentTextOrchestratesSuccessfulExtraction(t *testing.T) {
	svc, store, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte("image bytes")
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
		Metadata:    map[string]string{"source": "unit-test"},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	note := insertAttachmentNoteForTest(t, store, "proj", "projects/proj/report/ref.md", "refatt01")
	if err := store.LinkAttachmentToEntry(ctx, note.Path, mustParseAttachmentIDForTest(t, created.Attachment.ID), "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}

	extractor := &recordingAttachmentExtractor{resp: types.AttachmentExtractionResponse{
		Status:      types.AttachmentExtractionStatusReady,
		Text:        "extracted text from scan",
		Summary:     "short scan summary",
		Provider:    "test-provider",
		Model:       "test-model",
		ContentType: "text/markdown",
		DurationMs:  123,
		Metadata:    map[string]string{"page_count": "1"},
	}}
	svc.extractor = extractor

	result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{EntryID: note.ID})
	if err != nil {
		t.Fatalf("ExtractAttachmentText returned error: %v", err)
	}
	if len(extractor.calls) != 1 {
		t.Fatalf("extractor calls = %d, want 1", len(extractor.calls))
	}
	call := extractor.calls[0]
	if call.ProjectID != "proj" || call.EntryID != note.ID || call.AttachmentID != created.Attachment.ID || call.Filename != "scan.png" || call.ContentType != "image/png" || call.Size != int64(len(content)) {
		t.Fatalf("extractor request = %#v, want stored attachment fields plus caller entry", call)
	}
	if string(call.Content) != string(content) {
		t.Fatalf("extractor content = %q, want original bytes", call.Content)
	}
	if call.Metadata["source"] != "unit-test" {
		t.Fatalf("extractor metadata = %#v, want stored attachment metadata", call.Metadata)
	}
	if result.Attachment.ID != created.Attachment.ID {
		t.Fatalf("result attachment = %#v, want created attachment", result.Attachment)
	}
	if result.DerivedText.Status != types.AttachmentExtractionStatusReady || result.DerivedText.Text != "extracted text from scan" || result.DerivedText.ContentType != "text/markdown" {
		t.Fatalf("derived text = %#v, want ready extracted text", result.DerivedText)
	}
	metadata := result.DerivedText.Metadata
	for key, want := range map[string]string{
		"provider":              "test-provider",
		"model":                 "test-model",
		"original_content_type": "image/png",
		"elapsed_ms":            "123",
		"summary":               "short scan summary",
		"page_count":            "1",
	} {
		if metadata[key] != want {
			t.Fatalf("metadata[%q] = %q, want %q in %#v", key, metadata[key], want, metadata)
		}
	}
	if metadata["extracted_at"] == "" {
		t.Fatalf("metadata extracted_at missing in %#v", metadata)
	}
	if len(result.LinkedEntries) != 1 || result.LinkedEntries[0].Path != note.Path || result.LinkedEntries[0].Role != "inline" {
		t.Fatalf("linked entries = %#v, want linked note", result.LinkedEntries)
	}
	_, text, err := svc.OpenText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("OpenText after extraction returned error: %v", err)
	}
	defer text.Close()
	readBack, err := io.ReadAll(text)
	if err != nil {
		t.Fatalf("ReadAll derived text failed: %v", err)
	}
	if string(readBack) != "extracted text from scan" {
		t.Fatalf("OpenText read %q, want extracted text", readBack)
	}
}

func TestAttachmentServiceExtractAttachmentTextDisabledSkipsAndPreservesAttachment(t *testing.T) {
	svc, _, blobs := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	blobs.getCalls = nil

	result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
	if err != nil {
		t.Fatalf("ExtractAttachmentText returned error: %v", err)
	}
	if result.DerivedText.Status != types.AttachmentExtractionStatusSkipped || result.DerivedText.Error != "attachment extraction disabled" {
		t.Fatalf("derived = %#v, want skipped disabled status/error", result.DerivedText)
	}
	if len(blobs.getCalls) != 0 {
		t.Fatalf("blob Get calls = %#v, want none when extraction is disabled", blobs.getCalls)
	}
	status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("GetDerivedText returned error: %v", err)
	}
	if status == nil || status.Status != types.AttachmentExtractionStatusSkipped || status.Error != "attachment extraction disabled" {
		t.Fatalf("GetDerivedText = %#v, want visible skipped disabled status/error", status)
	}
	opened, stream, err := svc.Open(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("Open after disabled extraction returned error: %v", err)
	}
	defer stream.Close()
	readBack, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll original stream failed: %v", err)
	}
	if opened.ID != created.Attachment.ID || !bytes.Equal(readBack, content) {
		t.Fatalf("Open after disabled extraction = %#v/%q, want original attachment bytes", opened, readBack)
	}
}

func TestAttachmentServiceExtractAttachmentTextSkipsUnsupportedMIMEBeforeBlobRead(t *testing.T) {
	svc, _, blobs := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	extractor := &recordingAttachmentExtractor{resp: types.AttachmentExtractionResponse{Status: types.AttachmentExtractionStatusReady, Text: "should not run"}}
	svc.extractor = extractor
	svc.allowedMIME = normalizeMIMEPolicy([]string{"application/pdf"})
	blobs.getCalls = nil

	result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
	if err != nil {
		t.Fatalf("ExtractAttachmentText returned error: %v", err)
	}
	if len(extractor.calls) != 0 {
		t.Fatalf("extractor calls = %#v, want none for unsupported MIME", extractor.calls)
	}
	if len(blobs.getCalls) != 0 {
		t.Fatalf("blob Get calls = %#v, want none before unsupported MIME skip", blobs.getCalls)
	}
	if result.DerivedText.Status != types.AttachmentExtractionStatusSkipped || !strings.Contains(result.DerivedText.Error, "not allowed") {
		t.Fatalf("derived = %#v, want skipped unsupported MIME error", result.DerivedText)
	}
	status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("GetDerivedText returned error: %v", err)
	}
	if status == nil || status.Status != types.AttachmentExtractionStatusSkipped || !strings.Contains(status.Error, "not allowed") {
		t.Fatalf("GetDerivedText = %#v, want visible unsupported MIME status/error", status)
	}
}

func TestAttachmentServiceExtractAttachmentTextSkipsOversizedBeforeBlobRead(t *testing.T) {
	svc, _, blobs := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte("large enough for extraction limit")
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.pdf",
		ContentType: "application/pdf",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	extractor := &recordingAttachmentExtractor{resp: types.AttachmentExtractionResponse{Status: types.AttachmentExtractionStatusReady, Text: "should not run"}}
	svc.extractor = extractor
	svc.maxSizeBytes = int64(len(content) - 1)
	blobs.getCalls = nil

	result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
	if err != nil {
		t.Fatalf("ExtractAttachmentText returned error: %v", err)
	}
	if len(extractor.calls) != 0 {
		t.Fatalf("extractor calls = %#v, want none for oversized attachment", extractor.calls)
	}
	if len(blobs.getCalls) != 0 {
		t.Fatalf("blob Get calls = %#v, want none before oversized skip", blobs.getCalls)
	}
	if result.DerivedText.Status != types.AttachmentExtractionStatusSkipped || result.DerivedText.Error == "" {
		t.Fatalf("derived = %#v, want skipped oversized status/error", result.DerivedText)
	}
	if got, err := svc.Get(ctx, "proj", created.Attachment.ID); err != nil || got.ID != created.Attachment.ID {
		t.Fatalf("Get after oversized extraction skip = %#v/%v, want original attachment", got, err)
	}
}

func TestAttachmentServiceExtractAttachmentTextStoresExtractorSkippedAndFailedTransitions(t *testing.T) {
	ctx := context.Background()

	t.Run("extractor skipped", func(t *testing.T) {
		svc, _, _ := newAttachmentServiceForTest(t, 1024)
		created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "scan.pdf", ContentType: "application/pdf", Size: 4}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
		svc.extractor = &recordingAttachmentExtractor{resp: types.AttachmentExtractionResponse{
			Status:      types.AttachmentExtractionStatusSkipped,
			Text:        "ignored text",
			Error:       "model unavailable",
			Provider:    "test-provider",
			Model:       "test-model",
			ContentType: "text/markdown",
		}}

		result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
		if err != nil {
			t.Fatalf("ExtractAttachmentText returned error: %v", err)
		}
		if result.DerivedText.Status != types.AttachmentExtractionStatusSkipped || result.DerivedText.Error != "model unavailable" || result.DerivedText.Text != "" {
			t.Fatalf("derived = %#v, want skipped status/error with no text", result.DerivedText)
		}
		status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
		if err != nil {
			t.Fatalf("GetDerivedText returned error: %v", err)
		}
		if status == nil || status.Status != types.AttachmentExtractionStatusSkipped || status.Error != "model unavailable" || status.Metadata["provider"] != "test-provider" || status.Metadata["model"] != "test-model" {
			t.Fatalf("GetDerivedText = %#v, want visible skipped status/error metadata", status)
		}
	})

	t.Run("extractor failed", func(t *testing.T) {
		svc, _, _ := newAttachmentServiceForTest(t, 1024)
		created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "scan.pdf", ContentType: "application/pdf", Size: 4}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
		svc.extractor = &recordingAttachmentExtractor{err: errors.New("extractor crashed")}

		result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
		if err != nil {
			t.Fatalf("ExtractAttachmentText returned error: %v", err)
		}
		if result.DerivedText.Status != types.AttachmentExtractionStatusFailed || result.DerivedText.Error != "extractor crashed" || result.DerivedText.Text != "" {
			t.Fatalf("derived = %#v, want failed status/error with no text", result.DerivedText)
		}
		status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
		if err != nil {
			t.Fatalf("GetDerivedText returned error: %v", err)
		}
		if status == nil || status.Status != types.AttachmentExtractionStatusFailed || status.Error != "extractor crashed" {
			t.Fatalf("GetDerivedText = %#v, want visible failed status/error", status)
		}
		opened, stream, err := svc.Open(ctx, "proj", created.Attachment.ID)
		if err != nil {
			t.Fatalf("Open after extractor failure returned error: %v", err)
		}
		defer stream.Close()
		if opened.ID != created.Attachment.ID {
			t.Fatalf("Open after extractor failure = %#v, want original attachment", opened)
		}
	})

	t.Run("extractor failed response without error gets visible generic error", func(t *testing.T) {
		svc, _, _ := newAttachmentServiceForTest(t, 1024)
		created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "scan.pdf", ContentType: "application/pdf", Size: 4}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
		svc.extractor = &recordingAttachmentExtractor{resp: types.AttachmentExtractionResponse{Status: types.AttachmentExtractionStatusFailed}}

		result, err := svc.ExtractAttachmentText(ctx, "proj", created.Attachment.ID, types.AttachmentExtractionRequest{})
		if err != nil {
			t.Fatalf("ExtractAttachmentText returned error: %v", err)
		}
		if result.DerivedText.Status != types.AttachmentExtractionStatusFailed || result.DerivedText.Error == "" {
			t.Fatalf("derived = %#v, want failed status with visible generic error", result.DerivedText)
		}
		status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
		if err != nil {
			t.Fatalf("GetDerivedText returned error: %v", err)
		}
		if status == nil || status.Status != types.AttachmentExtractionStatusFailed || status.Error == "" {
			t.Fatalf("GetDerivedText = %#v, want visible failed status/error", status)
		}
	})
}

func TestAttachmentRowToDTOScaffold(t *testing.T) {
	att, err := attachmentRowToDTO(&storage.AttachmentRow{
		ID:        42,
		Digest:    "abc123",
		Size:      99,
		MediaType: "text/plain",
		Metadata:  `{"filename":"note.txt","project_id":"proj"}`,
		CreatedAt: "2026-05-23T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("attachmentRowToDTO returned error: %v", err)
	}
	want := types.Attachment{
		ID:          "42",
		Filename:    "note.txt",
		ContentType: "text/plain",
		Size:        99,
		SHA256:      "abc123",
		StorageKey:  "abc123",
		Created:     "2026-05-23T00:00:00Z",
		Modified:    "2026-05-23T00:00:00Z",
	}
	if att.ID != want.ID || att.Filename != want.Filename || att.ContentType != want.ContentType || att.Size != want.Size || att.SHA256 != want.SHA256 || att.StorageKey != want.StorageKey || att.Created != want.Created || att.Modified != want.Modified {
		t.Fatalf("attachmentRowToDTO = %#v, want fields %#v", att, want)
	}
	if att.Metadata["project_id"] != "proj" {
		t.Fatalf("metadata project_id = %q, want proj", att.Metadata["project_id"])
	}
}

func TestAttachmentServiceCreateGetOpenListAndDeleteUnreferenced(t *testing.T) {
	svc, store, blobs := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte("hello attachment")

	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "note.txt",
		ContentType: "",
		Size:        int64(len(content)),
		Metadata:    map[string]string{"purpose": "test"},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	att := created.Attachment
	if att.ID == "" || att.Filename != "note.txt" || att.Size != int64(len(content)) || att.SHA256 == "" || att.StorageKey != att.SHA256 {
		t.Fatalf("created attachment = %#v, want populated metadata", att)
	}
	if att.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("sniffed content type = %q, want text/plain; charset=utf-8", att.ContentType)
	}
	if att.Metadata["project_id"] != "proj" || att.Metadata["purpose"] != "test" {
		t.Fatalf("metadata = %#v, want project_id and purpose", att.Metadata)
	}

	got, err := svc.Get(ctx, "proj", att.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ID != att.ID || got.SHA256 != att.SHA256 {
		t.Fatalf("Get = %#v, want created attachment", got)
	}

	opened, stream, err := svc.Open(ctx, "proj", att.ID)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer stream.Close()
	readBack, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll stream failed: %v", err)
	}
	if opened.ID != att.ID || string(readBack) != string(content) {
		t.Fatalf("Open metadata/content = %#v/%q, want %s", opened, readBack, content)
	}

	list, err := svc.List(ctx, "proj")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if list.Total != 1 || len(list.Attachments) != 1 || list.Attachments[0].ID != att.ID {
		t.Fatalf("List = %#v, want one created attachment", list)
	}

	deleted, err := svc.Delete(ctx, "proj", att.ID)
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if !deleted {
		t.Fatal("Delete returned false for unreferenced attachment")
	}
	if _, ok := blobs.blobs[att.SHA256]; ok {
		t.Fatal("Delete left blob content after metadata delete succeeded")
	}
	row, err := store.GetAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("GetAttachment after delete failed: %v", err)
	}
	if row != nil {
		t.Fatalf("metadata row still exists after delete: %#v", row)
	}
}

func TestAttachmentServiceCreateEnforcesMIMEPolicy(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		blocked []string
		reqType string
		content string
	}{
		{
			name:    "rejects media type not on allow list",
			allowed: []string{"application/pdf"},
			reqType: "text/plain",
			content: "hello text",
		},
		{
			name:    "rejects blocked media type",
			blocked: []string{"text/plain"},
			reqType: "text/plain",
			content: "hello text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := storage.New(t.TempDir() + "/brain.db")
			if err != nil {
				t.Fatalf("storage.New failed: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			svc := NewAttachmentService(store, newRecordingBlobStore(), nil, 1024, WithAttachmentMIMEPolicy(tt.allowed, tt.blocked))

			_, err = svc.Create(context.Background(), "proj", types.CreateAttachmentRequest{
				Filename:    "note.txt",
				ContentType: tt.reqType,
				Size:        int64(len(tt.content)),
			}, strings.NewReader(tt.content))
			if err == nil {
				t.Fatal("Create returned nil error, want MIME policy rejection")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "mime type") {
				t.Fatalf("error = %v, want MIME policy error", err)
			}
		})
	}
}

func TestAttachmentServiceOpenTextReturnsTextualBlob(t *testing.T) {
	svc, _, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte("plain text attachment")
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "note.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	att, stream, err := svc.OpenText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("OpenText returned error: %v", err)
	}
	defer stream.Close()
	readBack, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll stream failed: %v", err)
	}
	if att.ID != created.Attachment.ID || string(readBack) != string(content) {
		t.Fatalf("OpenText metadata/content = %#v/%q, want %s", att, readBack, content)
	}
}

func TestAttachmentServiceOpenTextReturnsNotFoundForNonTextualBlob(t *testing.T) {
	svc, _, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "image.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	att, stream, err := svc.OpenText(ctx, "proj", created.Attachment.ID)
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("OpenText error = %v, want api.ErrNotFound", err)
	}
	if att != nil || stream != nil {
		t.Fatalf("OpenText returned attachment/stream for non-text: %#v/%#v", att, stream)
	}
}

func TestAttachmentServiceOpenTextPrefersReadyDerivedText(t *testing.T) {
	svc, _, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	stored, err := svc.StoreDerivedText(ctx, "proj", created.Attachment.ID, types.AttachmentDerivedText{
		Kind:        "text",
		Status:      "ready",
		ContentType: "text/plain; charset=utf-8",
		Text:        "ocr text from image",
		Metadata:    map[string]string{"extractor": "test"},
	})
	if err != nil {
		t.Fatalf("StoreDerivedText returned error: %v", err)
	}
	if stored.Status != "ready" || stored.Text != "ocr text from image" || stored.Metadata["extractor"] != "test" {
		t.Fatalf("StoreDerivedText = %#v, want ready derived text", stored)
	}

	att, stream, err := svc.OpenText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("OpenText returned error: %v", err)
	}
	defer stream.Close()
	readBack, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll stream failed: %v", err)
	}
	if att.ID != created.Attachment.ID || string(readBack) != "ocr text from image" {
		t.Fatalf("OpenText metadata/content = %#v/%q, want derived text", att, readBack)
	}
}

func TestAttachmentServiceFailedDerivedTextDoesNotBlockAttachmentBehavior(t *testing.T) {
	svc, _, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{
		Filename:    "scan.png",
		ContentType: "image/png",
		Size:        int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := svc.StoreDerivedText(ctx, "proj", created.Attachment.ID, types.AttachmentDerivedText{
		Kind:   "text",
		Status: "failed",
		Error:  "unsupported image format",
	}); err != nil {
		t.Fatalf("StoreDerivedText failed status returned error: %v", err)
	}

	if got, err := svc.Get(ctx, "proj", created.Attachment.ID); err != nil || got.ID != created.Attachment.ID {
		t.Fatalf("Get after failed extraction = %#v/%v, want original attachment", got, err)
	}
	opened, stream, err := svc.Open(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("Open after failed extraction returned error: %v", err)
	}
	_ = stream.Close()
	if opened.ID != created.Attachment.ID {
		t.Fatalf("Open after failed extraction = %#v, want original attachment", opened)
	}
	list, err := svc.List(ctx, "proj")
	if err != nil || list.Total != 1 {
		t.Fatalf("List after failed extraction = %#v/%v, want one original attachment", list, err)
	}
	status, err := svc.GetDerivedText(ctx, "proj", created.Attachment.ID)
	if err != nil {
		t.Fatalf("GetDerivedText returned error: %v", err)
	}
	if status == nil || status.Status != "failed" || status.Error != "unsupported image format" {
		t.Fatalf("GetDerivedText = %#v, want failed status", status)
	}
	if att, text, err := svc.OpenText(ctx, "proj", created.Attachment.ID); !errors.Is(err, api.ErrNotFound) || att != nil || text != nil {
		t.Fatalf("OpenText after failed extraction = %#v/%#v/%v, want api.ErrNotFound", att, text, err)
	}
}

func TestAttachmentServiceCreateValidationAndCleanup(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects unsafe filename before blob write", func(t *testing.T) {
		svc, _, blobs := newAttachmentServiceForTest(t, 1024)
		_, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "../note.txt", Size: 4}, strings.NewReader("data"))
		if err == nil || !strings.Contains(err.Error(), "filename") {
			t.Fatalf("Create error = %v, want filename validation error", err)
		}
		if blobs.putCalls != 0 {
			t.Fatalf("blob Put called %d times, want 0", blobs.putCalls)
		}
	})

	t.Run("rejects declared size over service max before blob write", func(t *testing.T) {
		svc, _, blobs := newAttachmentServiceForTest(t, 3)
		_, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
		if !errors.Is(err, blobstore.ErrTooLarge) {
			t.Fatalf("Create error = %v, want ErrTooLarge", err)
		}
		if blobs.putCalls != 0 {
			t.Fatalf("blob Put called %d times, want 0", blobs.putCalls)
		}
	})

	t.Run("does not write metadata when blob put fails", func(t *testing.T) {
		svc, store, blobs := newAttachmentServiceForTest(t, 1024)
		blobs.putErr = errors.New("disk full")
		_, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("Create error = %v, want blob error", err)
		}
		rows, listErr := store.ListAttachments(ctx)
		if listErr != nil {
			t.Fatalf("ListAttachments failed: %v", listErr)
		}
		if len(rows) != 0 {
			t.Fatalf("metadata rows = %#v, want none", rows)
		}
	})

	t.Run("deletes new blob when metadata write fails", func(t *testing.T) {
		svc, store, blobs := newAttachmentServiceForTest(t, 1024)
		if err := store.Close(); err != nil {
			t.Fatalf("Close storage failed: %v", err)
		}
		_, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
		if err == nil {
			t.Fatal("Create returned nil error after metadata store was closed")
		}
		if len(blobs.blobs) != 0 {
			t.Fatalf("blob map = %#v, want cleanup after metadata failure", blobs.blobs)
		}
		if len(blobs.deleteCall) != 1 {
			t.Fatalf("blob delete calls = %#v, want one cleanup delete", blobs.deleteCall)
		}
	})

	t.Run("does not delete existing blob when metadata write fails", func(t *testing.T) {
		svc, store, blobs := newAttachmentServiceForTest(t, 1024)
		created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("initial Create returned error: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close storage failed: %v", err)
		}

		_, err = svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "again.txt", Size: 4}, strings.NewReader("data"))
		if err == nil {
			t.Fatal("Create returned nil error after metadata store was closed")
		}
		if _, ok := blobs.blobs[created.Attachment.SHA256]; !ok {
			t.Fatal("metadata failure cleanup deleted pre-existing blob")
		}
	})

	t.Run("rejects mismatched declared sha256", func(t *testing.T) {
		svc, _, blobs := newAttachmentServiceForTest(t, 1024)
		_, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4, SHA256: strings.Repeat("0", 64)}, strings.NewReader("data"))
		if err == nil || !strings.Contains(err.Error(), "sha256") {
			t.Fatalf("Create error = %v, want sha256 mismatch", err)
		}
		if blobs.putCalls != 0 {
			t.Fatalf("blob Put called %d times, want 0", blobs.putCalls)
		}
	})
}

func TestAttachmentServiceDeleteRefusesReferencedAndPreservesBlob(t *testing.T) {
	svc, store, blobs := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	att := created.Attachment
	note, err := store.InsertNote(ctx, &storage.NoteRow{Path: "projects/proj/report/ref.md", ShortID: "refatt01", Title: "Referenced"})
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, note.Path, mustParseAttachmentIDForTest(t, att.ID), "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}

	deleted, err := svc.Delete(ctx, "proj", att.ID)
	if err != nil {
		t.Fatalf("Delete returned error for referenced attachment: %v", err)
	}
	if deleted {
		t.Fatal("Delete returned true for referenced attachment")
	}
	if len(blobs.deleteCall) != 0 {
		t.Fatalf("blob delete calls = %#v, want none while referenced", blobs.deleteCall)
	}
	if _, ok := blobs.blobs[att.SHA256]; !ok {
		t.Fatal("referenced attachment blob was deleted")
	}
}

func TestAttachmentServiceAttachUpdatesStorageAndEntryFrontmatter(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{
		ID:        "entry123",
		Path:      "projects/proj/report/ref.md",
		Title:     "Referenced",
		ProjectID: "proj",
		Attachments: []types.AttachmentReference{{
			ID: "existing", Role: "source", Caption: "keep me",
		}},
	}
	brain := newMockBrainForAttachments(entry)
	svc, store, _ := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")

	resp, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{
		Attachment: types.AttachmentReference{ID: att.ID, Role: "inline", Caption: "diagram"},
	})
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	if resp.EntryID != entry.ID || resp.Path != entry.Path {
		t.Fatalf("Attach response identity = %#v, want entry id/path", resp)
	}
	if len(resp.Attachments) != 2 {
		t.Fatalf("Attach response attachments = %#v, want existing plus new", resp.Attachments)
	}
	got := resp.Attachments[1]
	if got.ID != att.ID || got.Role != "inline" || got.Caption != "diagram" || got.Filename != "note.txt" || got.SHA256 != att.SHA256 || got.Size != 4 {
		t.Fatalf("new attachment ref = %#v, want enriched ref", got)
	}
	if len(brain.updates) != 1 || brain.updates[0].Attachments == nil || len(*brain.updates[0].Attachments) != 2 {
		t.Fatalf("Brain Update calls = %#v, want one attachments update", brain.updates)
	}
	refs, err := store.ListEntryReferencesForAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 1 || refs[0].NotePath != entry.Path || refs[0].Role != "inline" {
		t.Fatalf("storage refs = %#v, want inline link", refs)
	}
}

func TestAttachmentServiceAttachIsIdempotentForSameAttachmentAndRole(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{ID: "entry123", Path: "projects/proj/report/ref.md", Title: "Referenced", ProjectID: "proj"}
	brain := newMockBrainForAttachments(entry)
	svc, store, _ := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")
	req := types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: att.ID, Role: "inline", Caption: "first"}}
	if _, err := svc.Attach(ctx, "proj", entry.ID, req); err != nil {
		t.Fatalf("first Attach returned error: %v", err)
	}
	req.Attachment.Caption = "replacement caption"
	resp, err := svc.Attach(ctx, "proj", entry.ID, req)
	if err != nil {
		t.Fatalf("second Attach returned error: %v", err)
	}
	if len(resp.Attachments) != 1 {
		t.Fatalf("attachments after duplicate attach = %#v, want one", resp.Attachments)
	}
	if resp.Attachments[0].Caption != "replacement caption" {
		t.Fatalf("caption = %q, want replacement caption", resp.Attachments[0].Caption)
	}
	refs, err := store.ListEntryReferencesForAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("storage refs = %#v, want single idempotent link", refs)
	}
}

func TestAttachmentServiceAttachRollsBackStorageLinkWhenEntryUpdateFails(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{ID: "entry123", Path: "projects/proj/report/ref.md", Title: "Referenced", ProjectID: "proj"}
	brain := newMockBrainForAttachments(entry)
	brain.updateErr = errors.New("update failed")
	svc, store, _ := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")

	_, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{
		Attachment: types.AttachmentReference{ID: att.ID, Role: "inline"},
	})
	if err == nil || !strings.Contains(err.Error(), "update failed") {
		t.Fatalf("Attach error = %v, want update failure", err)
	}
	refs, err := store.ListEntryReferencesForAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("storage refs after rollback = %#v, want none", refs)
	}
}

func TestAttachmentServiceDetachUpdatesFrontmatterAndAllowsDelete(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{ID: "entry123", Path: "projects/proj/report/ref.md", Title: "Referenced", ProjectID: "proj"}
	brain := newMockBrainForAttachments(entry)
	svc, store, blobs := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")
	if _, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: att.ID, Role: "inline"}}); err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}

	resp, err := svc.Detach(ctx, "proj", entry.ID, att.ID, "inline")
	if err != nil {
		t.Fatalf("Detach returned error: %v", err)
	}
	if len(resp.Attachments) != 0 {
		t.Fatalf("Detach response attachments = %#v, want none", resp.Attachments)
	}
	refs, err := store.ListEntryReferencesForAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("storage refs after detach = %#v, want none", refs)
	}
	deleted, err := svc.Delete(ctx, "proj", att.ID)
	if err != nil {
		t.Fatalf("Delete after detach returned error: %v", err)
	}
	if !deleted {
		t.Fatal("Delete after detach returned false, want true")
	}
	if _, ok := blobs.blobs[att.SHA256]; ok {
		t.Fatal("blob remains after delete following detach")
	}
}

func TestAttachmentServiceDetachRollsBackStorageUnlinkWhenEntryUpdateFails(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{ID: "entry123", Path: "projects/proj/report/ref.md", Title: "Referenced", ProjectID: "proj"}
	brain := newMockBrainForAttachments(entry)
	svc, store, _ := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")
	if _, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: att.ID, Role: "inline"}}); err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	brain.updateErr = fmt.Errorf("update failed")

	_, err := svc.Detach(ctx, "proj", entry.ID, att.ID, "inline")
	if err == nil || !strings.Contains(err.Error(), "update failed") {
		t.Fatalf("Detach error = %v, want update failure", err)
	}
	refs, err := store.ListEntryReferencesForAttachment(ctx, mustParseAttachmentIDForTest(t, att.ID))
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 1 || refs[0].Role != "inline" {
		t.Fatalf("storage refs after rollback = %#v, want restored inline link", refs)
	}
}

func TestAttachmentServiceAttachDetachValidationErrors(t *testing.T) {
	ctx := context.Background()
	entry := &types.BrainEntry{ID: "entry123", Path: "projects/proj/report/ref.md", Title: "Referenced", ProjectID: "proj"}
	brain := newMockBrainForAttachments(entry)
	svc, store, _ := newAttachmentServiceWithBrainForTest(t, brain)
	insertAttachmentNoteForTest(t, store, "proj", entry.Path, entry.ID)
	att := createAttachmentForServiceTest(t, svc, "proj", "note.txt")

	if _, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: "../1", Role: "inline"}}); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("Attach unsafe ID error = %v, want unsafe", err)
	}
	if _, err := svc.Attach(ctx, "proj", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: att.ID, Role: "bad/role"}}); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("Attach unsafe role error = %v, want role", err)
	}
	if _, err := svc.Attach(ctx, "other", entry.ID, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: att.ID, Role: "inline"}}); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Attach wrong project error = %v, want api.ErrNotFound", err)
	}
	if _, err := svc.Detach(ctx, "proj", entry.ID, "../1", "inline"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("Detach unsafe ID error = %v, want unsafe", err)
	}
	if _, err := svc.Detach(ctx, "proj", entry.ID, att.ID, "bad/role"); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("Detach unsafe role error = %v, want role", err)
	}
}

func TestAttachmentServiceRejectsUnsafeIDsAndWrongProject(t *testing.T) {
	svc, _, _ := newAttachmentServiceForTest(t, 1024)
	ctx := context.Background()
	created, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "note.txt", Size: 4}, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if _, err := svc.Get(ctx, "proj", "../1"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("Get unsafe ID error = %v, want unsafe error", err)
	}
	if _, err := svc.Get(ctx, "other", created.Attachment.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Get wrong project error = %v, want api.ErrNotFound", err)
	}
	if _, _, err := svc.Open(ctx, "other", created.Attachment.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Open wrong project error = %v, want api.ErrNotFound", err)
	}
	if deleted, err := svc.Delete(ctx, "other", created.Attachment.ID); !errors.Is(err, api.ErrNotFound) || deleted {
		t.Fatalf("Delete wrong project = %v/%v, want false api.ErrNotFound", deleted, err)
	}
}

func mustParseAttachmentIDForTest(t *testing.T, id string) int64 {
	t.Helper()
	parsed, err := parseAttachmentID(id)
	if err != nil {
		t.Fatalf("parseAttachmentID(%q) failed: %v", id, err)
	}
	return parsed
}
