package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

type mockAttachmentService struct {
	getCalled      bool
	listCalled     bool
	attachCalled   bool
	detachCalled   bool
	deleteCalled   bool
	createCalled   bool
	openCalled     bool
	textCalled     bool
	extractCalled  bool
	backfillCalled bool

	projectID    string
	entryID      string
	attachmentID string
	role         string
	createReq    types.CreateAttachmentRequest
	attachReq    types.AttachEntryAttachmentRequest
	extractReq   types.AttachmentExtractionRequest
	backfillReq  types.AttachmentExtractionBackfillRequest
	createBody   []byte

	createErr       error
	getErr          error
	listErr         error
	listForEntryErr error
	attachErr       error
	detachErr       error
	deleteErr       error
	deleteResult    *bool
	openErr         error
	textErr         error
	extractErr      error
	backfillErr     error
	extractResult   *types.AttachmentExtractionResult
	backfillResult  *types.AttachmentExtractionBackfillResponse
}

func (m *mockAttachmentService) Create(ctx context.Context, projectID string, req types.CreateAttachmentRequest, content io.Reader) (*types.CreateAttachmentResponse, error) {
	m.createCalled = true
	m.projectID = projectID
	m.createReq = req
	if content != nil {
		m.createBody, _ = io.ReadAll(content)
	}
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &types.CreateAttachmentResponse{Attachment: types.Attachment{ID: "att_created", Filename: req.Filename, ContentType: req.ContentType, Size: req.Size, Metadata: req.Metadata}}, nil
}

func (m *mockAttachmentService) Get(ctx context.Context, projectID, attachmentID string) (*types.Attachment, error) {
	m.getCalled = true
	m.projectID = projectID
	m.attachmentID = attachmentID
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &types.Attachment{ID: attachmentID, Filename: "test.txt", ContentType: "text/plain"}, nil
}

func (m *mockAttachmentService) Open(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error) {
	m.openCalled = true
	m.projectID = projectID
	m.attachmentID = attachmentID
	if m.openErr != nil {
		return nil, nil, m.openErr
	}
	return &types.Attachment{ID: attachmentID, Filename: "unsafe name \"x\".txt", ContentType: "text/plain", Size: 11}, io.NopCloser(strings.NewReader("hello world")), nil
}

func (m *mockAttachmentService) OpenText(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error) {
	m.textCalled = true
	m.projectID = projectID
	m.attachmentID = attachmentID
	if m.textErr != nil {
		return nil, nil, m.textErr
	}
	return &types.Attachment{ID: attachmentID, Filename: "note.txt", ContentType: "text/plain; charset=utf-8", Size: 11}, io.NopCloser(strings.NewReader("hello world")), nil
}

func (m *mockAttachmentService) StoreDerivedText(ctx context.Context, projectID, attachmentID string, derived types.AttachmentDerivedText) (*types.AttachmentDerivedText, error) {
	m.projectID = projectID
	m.attachmentID = attachmentID
	return &derived, nil
}

func (m *mockAttachmentService) GetDerivedText(ctx context.Context, projectID, attachmentID string) (*types.AttachmentDerivedText, error) {
	m.projectID = projectID
	m.attachmentID = attachmentID
	return nil, nil
}

func (m *mockAttachmentService) ExtractAttachmentText(ctx context.Context, projectID, attachmentID string, req types.AttachmentExtractionRequest) (*types.AttachmentExtractionResult, error) {
	m.extractCalled = true
	m.projectID = projectID
	m.attachmentID = attachmentID
	m.extractReq = req
	if m.extractErr != nil {
		return nil, m.extractErr
	}
	if m.extractResult != nil {
		return m.extractResult, nil
	}
	return &types.AttachmentExtractionResult{
		Attachment: types.Attachment{ID: attachmentID, Filename: "scan.pdf", ContentType: "application/pdf"},
		DerivedText: types.AttachmentDerivedText{
			ID:          "derived_123",
			Kind:        "extracted_text",
			Status:      types.AttachmentExtractionStatusReady,
			ContentType: "text/markdown; charset=utf-8",
			Text:        "# Extracted text",
			Metadata: map[string]string{
				"provider": "openrouter",
				"model":    "google/gemini-2.5-flash",
			},
		},
		LinkedEntries: []types.AttachmentLinkedEntry{{Path: "projects/test-project/report/entry.md", Role: "inline"}},
	}, nil
}

func (m *mockAttachmentService) BackfillAttachmentExtraction(ctx context.Context, projectID string, req types.AttachmentExtractionBackfillRequest) (*types.AttachmentExtractionBackfillResponse, error) {
	m.backfillCalled = true
	m.projectID = projectID
	m.backfillReq = req
	if m.backfillErr != nil {
		return nil, m.backfillErr
	}
	if m.backfillResult != nil {
		return m.backfillResult, nil
	}
	return &types.AttachmentExtractionBackfillResponse{DryRun: req.DryRun}, nil
}

func (m *mockAttachmentService) List(ctx context.Context, projectID string) (*types.ListAttachmentsResponse, error) {
	m.listCalled = true
	m.projectID = projectID
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &types.ListAttachmentsResponse{Attachments: []types.Attachment{{ID: "att_123", Filename: "test.txt", ContentType: "text/plain"}}, Total: 1}, nil
}

func (m *mockAttachmentService) ListForEntry(ctx context.Context, projectID, pathOrID string) (*types.AttachEntryAttachmentResponse, error) {
	m.listCalled = true
	m.projectID = projectID
	m.entryID = pathOrID
	if m.listForEntryErr != nil {
		return nil, m.listForEntryErr
	}
	return &types.AttachEntryAttachmentResponse{
		EntryID: "entry-123",
		Path:    "projects/test-project/report/entry.md",
		Attachments: []types.AttachmentReference{{
			ID: "att_123", Filename: "test.txt", ContentType: "text/plain", Role: "inline",
		}},
	}, nil
}

func (m *mockAttachmentService) Attach(ctx context.Context, projectID, pathOrID string, req types.AttachEntryAttachmentRequest) (*types.AttachEntryAttachmentResponse, error) {
	m.attachCalled = true
	m.projectID = projectID
	m.entryID = pathOrID
	m.attachReq = req
	if m.attachErr != nil {
		return nil, m.attachErr
	}
	return &types.AttachEntryAttachmentResponse{EntryID: pathOrID, Path: "projects/" + projectID + "/report/entry.md", Attachments: []types.AttachmentReference{req.Attachment}}, nil
}

func (m *mockAttachmentService) Detach(ctx context.Context, projectID, pathOrID, attachmentID, role string) (*types.AttachEntryAttachmentResponse, error) {
	m.detachCalled = true
	m.projectID = projectID
	m.entryID = pathOrID
	m.attachmentID = attachmentID
	m.role = role
	if m.detachErr != nil {
		return nil, m.detachErr
	}
	return &types.AttachEntryAttachmentResponse{Path: pathOrID}, nil
}

func (m *mockAttachmentService) Delete(ctx context.Context, projectID, attachmentID string) (bool, error) {
	m.deleteCalled = true
	m.projectID = projectID
	m.attachmentID = attachmentID
	if m.deleteErr != nil {
		return false, m.deleteErr
	}
	if m.deleteResult != nil {
		return *m.deleteResult, nil
	}
	return true, nil
}

func TestWithAttachmentServiceSetsHandlerDependency(t *testing.T) {
	attachments := &mockAttachmentService{}
	h := NewHandler(&mockBrainService{}, WithAttachmentService(attachments))
	if h.attachments != attachments {
		t.Fatal("WithAttachmentService did not set handler attachment service")
	}
}

func TestAttachmentTopLevelRoutesAreRegistered(t *testing.T) {
	attachments := &mockAttachmentService{}
	h := NewHandler(&mockBrainService{}, WithAttachmentService(attachments))
	router := NewRouter(config.Config{}, WithHandler(h))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/att_123?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if !attachments.getCalled {
		t.Fatal("GET /api/v1/attachments/{attachmentID}?project_id=... did not dispatch to attachment service")
	}
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("attachment metadata route was not registered, status = %d", rec.Code)
	}
}

func TestDeleteAttachmentRouteDispatchesToService(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/att_123?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.deleteCalled || attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("Delete called=%v projectID=%q attachmentID=%q", attachments.deleteCalled, attachments.projectID, attachments.attachmentID)
	}
	if !strings.Contains(rec.Body.String(), `"deleted":true`) {
		t.Fatalf("body = %s, want deleted true", rec.Body.String())
	}
}

func TestDeleteAttachmentRouteReturnsConflictWhenReferenced(t *testing.T) {
	deleteResult := false
	attachments := &mockAttachmentService{deleteResult: &deleteResult}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/att_123?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "referenced") {
		t.Fatalf("body = %s, want referenced deletion refusal", rec.Body.String())
	}
}

func TestCreateAttachmentMultipartUpload(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", "test-project"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("metadata", `{"kind":"fixture"}`); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.createCalled {
		t.Fatal("Create was not called")
	}
	if attachments.projectID != "test-project" {
		t.Fatalf("projectID = %q", attachments.projectID)
	}
	if attachments.createReq.Filename != "hello.txt" {
		t.Fatalf("filename = %q", attachments.createReq.Filename)
	}
	if attachments.createReq.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", attachments.createReq.ContentType)
	}
	if attachments.createReq.Size != int64(len("hello world")) {
		t.Fatalf("size = %d", attachments.createReq.Size)
	}
	if attachments.createReq.Metadata["kind"] != "fixture" {
		t.Fatalf("metadata = %#v", attachments.createReq.Metadata)
	}
	if string(attachments.createBody) != "hello world" {
		t.Fatalf("content body = %q", string(attachments.createBody))
	}
}

func TestCreateAttachmentMapsTooLargeError(t *testing.T) {
	attachments := &mockAttachmentService{createErr: blobstore.ErrTooLarge}
	router := newAttachmentTestRouter(attachments)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", "test-project"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("too large")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestCreateAttachmentMapsMIMEPolicyError(t *testing.T) {
	attachments := &mockAttachmentService{createErr: errors.New(`attachment MIME type "application/x-msdownload" is blocked`)}
	router := newAttachmentTestRouter(attachments)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", "test-project"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "blocked.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("blocked")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
	}
}

func TestListAttachmentMetadataUsesProjectIDQuery(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.listCalled || attachments.projectID != "test-project" {
		t.Fatalf("List called = %v, projectID = %q", attachments.listCalled, attachments.projectID)
	}
	var resp types.ListAttachmentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Attachments[0].ID != "att_123" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestGetAttachmentMetadataUsesProjectIDQuery(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/att_123?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.getCalled || attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("Get called = %v, projectID = %q, attachmentID = %q", attachments.getCalled, attachments.projectID, attachments.attachmentID)
	}
}

func TestDownloadAttachmentStreamsBytesWithSafeHeaders(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/att_123/content?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.openCalled || attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("Open called = %v, projectID = %q, attachmentID = %q", attachments.openCalled, attachments.projectID, attachments.attachmentID)
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	contentDisposition := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(contentDisposition, `attachment; filename="unsafe_name__x_.txt"`) {
		t.Fatalf("Content-Disposition = %q", contentDisposition)
	}
}

func TestGetAttachmentTextStreamsPlainText(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/att_123/text?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.textCalled || attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("OpenText called = %v, projectID = %q, attachmentID = %q", attachments.textCalled, attachments.projectID, attachments.attachmentID)
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestExtractAttachmentRouteDispatchesToServiceWithEmptyBody(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/att_123/extract?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.extractCalled || attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("Extract called = %v, projectID = %q, attachmentID = %q", attachments.extractCalled, attachments.projectID, attachments.attachmentID)
	}
	if attachments.extractReq.ProjectID != "" || attachments.extractReq.AttachmentID != "" {
		t.Fatalf("path/query should be authoritative, req = %#v", attachments.extractReq)
	}
	var resp types.AttachmentExtractionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DerivedText.Status != types.AttachmentExtractionStatusReady || resp.DerivedText.Metadata["provider"] != "openrouter" || resp.DerivedText.Metadata["model"] == "" {
		t.Fatalf("response derived text = %#v", resp.DerivedText)
	}
	if len(resp.LinkedEntries) != 1 || resp.LinkedEntries[0].Path == "" {
		t.Fatalf("linked entries = %#v", resp.LinkedEntries)
	}
}

func TestExtractAttachmentRouteParsesOptionalJSONBody(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)
	body := strings.NewReader(`{"project_id":"ignored-project","attachment_id":"ignored-att","entry_id":"entry-123","filename":"scan.pdf","content_type":"application/pdf","size":42,"metadata":{"source":"test"}}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/att_123/extract?project_id=test-project", body)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if attachments.projectID != "test-project" || attachments.attachmentID != "att_123" {
		t.Fatalf("service args projectID = %q, attachmentID = %q", attachments.projectID, attachments.attachmentID)
	}
	if attachments.extractReq.ProjectID != "ignored-project" || attachments.extractReq.AttachmentID != "ignored-att" || attachments.extractReq.EntryID != "entry-123" {
		t.Fatalf("extractReq = %#v", attachments.extractReq)
	}
	if attachments.extractReq.Metadata["source"] != "test" {
		t.Fatalf("metadata = %#v", attachments.extractReq.Metadata)
	}
}

func TestExtractAttachmentRouteRequiresProjectID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/att_123/extract", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if attachments.extractCalled {
		t.Fatal("ExtractAttachmentText should not be called without project_id")
	}
}

func TestExtractAttachmentRouteMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "not found", err: ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "bad request", err: errors.New("attachment content_type is required"), wantStatus: http.StatusBadRequest},
		{name: "internal", err: errors.New("extractor unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments := &mockAttachmentService{extractErr: tt.err}
			router := newAttachmentTestRouter(attachments)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/att_123/extract?project_id=test-project", nil)
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestExtractAttachmentRouteDoesNotShadowContentOrTextRoutes(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	for _, tt := range []struct {
		name      string
		path      string
		wantCheck func(*mockAttachmentService) bool
	}{
		{name: "content", path: "/api/v1/attachments/att_123/content?project_id=test-project", wantCheck: func(m *mockAttachmentService) bool { return m.openCalled && !m.extractCalled }},
		{name: "text", path: "/api/v1/attachments/att_123/text?project_id=test-project", wantCheck: func(m *mockAttachmentService) bool { return m.textCalled && !m.extractCalled }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			*attachments = mockAttachmentService{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !tt.wantCheck(attachments) {
				t.Fatalf("route dispatch state = %#v", attachments)
			}
		})
	}
}

func TestBackfillAttachmentExtractionRouteDispatchesToService(t *testing.T) {
	attachments := &mockAttachmentService{backfillResult: &types.AttachmentExtractionBackfillResponse{
		Total:      3,
		Candidates: 2,
		Processed:  1,
		Skipped:    1,
		Failed:     1,
		DryRun:     true,
		Attachments: []types.AttachmentExtractionBackfillItem{
			{AttachmentID: "att_ready", Status: types.AttachmentExtractionStatusReady},
			{AttachmentID: "att_failed", Status: types.AttachmentExtractionStatusFailed, Error: "extract failed"},
		},
	}}
	router := newAttachmentTestRouter(attachments)
	body := strings.NewReader(`{"dry_run":true,"force":true,"batch_size":5,"rate_limit_delay_ms":25}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/backfill/extraction?project_id=test-project", body)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.backfillCalled || attachments.projectID != "test-project" {
		t.Fatalf("Backfill called = %v, projectID = %q", attachments.backfillCalled, attachments.projectID)
	}
	if !attachments.backfillReq.DryRun || !attachments.backfillReq.Force || attachments.backfillReq.BatchSize != 5 || attachments.backfillReq.RateLimitDelayMs != 25 {
		t.Fatalf("backfillReq = %#v", attachments.backfillReq)
	}
	var resp types.AttachmentExtractionBackfillResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Failed != 1 || resp.Processed != 1 || len(resp.Attachments) != 2 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestBackfillAttachmentExtractionRouteRequiresProjectID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/backfill/extraction", strings.NewReader(`{"dry_run":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if attachments.backfillCalled {
		t.Fatal("BackfillAttachmentExtraction should not be called without project_id")
	}
}

func TestBackfillAttachmentExtractionRouteRejectsInvalidJSON(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/backfill/extraction?project_id=test-project", strings.NewReader(`{"dry_run":`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if attachments.backfillCalled {
		t.Fatal("BackfillAttachmentExtraction should not be called for invalid JSON")
	}
}

func TestBackfillAttachmentExtractionRouteMapsServiceErrors(t *testing.T) {
	attachments := &mockAttachmentService{backfillErr: errors.New("batch_size must be positive")}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/backfill/extraction?project_id=test-project", strings.NewReader(`{"batch_size":-1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAttachmentRequiresMultipartFileAndProjectID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	tests := []struct {
		name        string
		projectID   string
		includeFile bool
	}{
		{name: "missing project_id", includeFile: true},
		{name: "missing file", projectID: "test-project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			if tt.projectID != "" {
				if err := writer.WriteField("project_id", tt.projectID); err != nil {
					t.Fatal(err)
				}
			}
			if tt.includeFile {
				part, err := writer.CreateFormFile("file", "hello.txt")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = part.Write([]byte("hello world"))
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAttachmentMetadataEndpointsRequireProjectID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	for _, path := range []string{
		"/api/v1/attachments",
		"/api/v1/attachments/att_123",
		"/api/v1/attachments/att_123/content",
		"/api/v1/attachments/att_123/text",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetAttachmentTextMapsNotFoundError(t *testing.T) {
	attachments := &mockAttachmentService{textErr: ErrNotFound}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/missing/text?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetAttachmentMetadataMapsNotFoundError(t *testing.T) {
	attachments := &mockAttachmentService{getErr: ErrNotFound}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/missing?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListAttachmentMetadataMapsServiceErrors(t *testing.T) {
	attachments := &mockAttachmentService{listErr: errors.New("storage failed")}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/attachments?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func newAttachmentTestRouter(attachments *mockAttachmentService) http.Handler {
	h := NewHandler(&mockBrainService{}, WithAttachmentService(attachments))
	return NewRouter(config.Config{}, WithHandler(h))
}

// newAttachmentTestRouterWithEvents wires an event service alongside the
// attachment service so attachment event emission can be observed.
func newAttachmentTestRouterWithEvents(attachments *mockAttachmentService, es *mockEventService) http.Handler {
	h := NewHandler(&mockBrainService{},
		WithAttachmentService(attachments),
		WithEventService(es),
	)
	return NewRouter(config.Config{}, WithHandler(h))
}

// TestAttachmentEventTypesAreRegistered guards the failure mode that made
// webhook.received unreachable for years: an event type that exists as a
// constant but is missing from AllEventTypes is rejected by
// EventServiceImpl.Ingest and makes POST /api/v1/events return 400. The
// mock event service does not validate, so without this the handler tests
// below would pass against an unusable event type.
func TestAttachmentEventTypesAreRegistered(t *testing.T) {
	for _, eventType := range []string{types.EventAttachmentCreated, types.EventEntryAttachmentAdded} {
		if !types.IsValidEventType(eventType) {
			t.Errorf("IsValidEventType(%q) = false; add it to types.AllEventTypes or Ingest will reject it", eventType)
		}
	}
}

func TestCreateAttachmentEmitsAttachmentCreatedEvent(t *testing.T) {
	attachments := &mockAttachmentService{}
	es := &mockEventService{}
	router := newAttachmentTestRouterWithEvents(attachments, es)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", "test-project"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "page.png")
	if err != nil {
		t.Fatal(err)
	}
	// PNG magic bytes so DetectContentType reports image/png.
	payload := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventAttachmentCreated {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventAttachmentCreated)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "test-project" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "test-project")
	}
	if evt.Metadata["attachment_id"] != "att_created" {
		t.Errorf("metadata[attachment_id] = %q, want %q", evt.Metadata["attachment_id"], "att_created")
	}
	if evt.Metadata["filename"] != "page.png" {
		t.Errorf("metadata[filename] = %q, want %q", evt.Metadata["filename"], "page.png")
	}
	if evt.Metadata["media_type"] != "image/png" {
		t.Errorf("metadata[media_type] = %q, want %q", evt.Metadata["media_type"], "image/png")
	}
	wantSize := strconv.Itoa(len(payload))
	if evt.Metadata["size_bytes"] != wantSize {
		t.Errorf("metadata[size_bytes] = %q, want %q", evt.Metadata["size_bytes"], wantSize)
	}
}

// TestCreateAttachmentDoesNotEmitEventOnFailure keeps the emitter on the
// success path only — a rejected upload must not look like a stored one.
func TestCreateAttachmentDoesNotEmitEventOnFailure(t *testing.T) {
	attachments := &mockAttachmentService{createErr: errors.New("boom")}
	es := &mockEventService{}
	router := newAttachmentTestRouterWithEvents(attachments, es)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", "test-project"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "page.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusCreated {
		t.Fatalf("expected failure status, got %d", rec.Code)
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 0 {
		t.Fatalf("ingested events = %d, want 0 on failure", len(es.ingested))
	}
}

func TestAttachEntryAttachmentEmitsEntryAttachmentAddedEvent(t *testing.T) {
	attachments := &mockAttachmentService{}
	es := &mockEventService{}
	router := newAttachmentTestRouterWithEvents(attachments, es)

	payload := `{"project_id":"test-project","attachment":{"id":"att_9","content_type":"image/png","role":"source"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entries/entry-123/attachments", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventEntryAttachmentAdded {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventEntryAttachmentAdded)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "test-project" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "test-project")
	}
	// TaskID/TaskPath carry the ENTRY identity so once_per: task_id and
	// {{.TaskPath}} work for attachment-driven automations too.
	if evt.TaskID != "entry-123" {
		t.Errorf("task_id = %q, want %q", evt.TaskID, "entry-123")
	}
	if evt.TaskPath != "projects/test-project/report/entry.md" {
		t.Errorf("task_path = %q, want %q", evt.TaskPath, "projects/test-project/report/entry.md")
	}
	if evt.Metadata["attachment_id"] != "att_9" {
		t.Errorf("metadata[attachment_id] = %q, want %q", evt.Metadata["attachment_id"], "att_9")
	}
	if evt.Metadata["media_type"] != "image/png" {
		t.Errorf("metadata[media_type] = %q, want %q", evt.Metadata["media_type"], "image/png")
	}
	if evt.Metadata["role"] != "source" {
		t.Errorf("metadata[role] = %q, want %q", evt.Metadata["role"], "source")
	}
}

// TestAttachEntryAttachmentDoesNotEmitEventOnFailure keeps the emitter on the
// success path only.
func TestAttachEntryAttachmentDoesNotEmitEventOnFailure(t *testing.T) {
	attachments := &mockAttachmentService{attachErr: errors.New("boom")}
	es := &mockEventService{}
	router := newAttachmentTestRouterWithEvents(attachments, es)

	payload := `{"project_id":"test-project","attachment":{"id":"att_9"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entries/entry-123/attachments", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected failure status, got %d", rec.Code)
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 0 {
		t.Fatalf("ingested events = %d, want 0 on failure", len(es.ingested))
	}
}

func TestEntryAttachmentRoutesAreNotShadowedByEntryWildcardRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wasCalled  func(*mockAttachmentService) bool
		calledName string
	}{
		{
			name:       "list entry attachments",
			method:     http.MethodGet,
			path:       "/api/v1/entries/entry-123/attachments?project_id=test-project",
			wasCalled:  func(m *mockAttachmentService) bool { return m.listCalled },
			calledName: "List",
		},
		{
			name:       "attach entry attachment",
			method:     http.MethodPost,
			path:       "/api/v1/entries/entry-123/attachments?project_id=test-project",
			body:       `{"attachment":{"id":"att_123","role":"inline"}}`,
			wasCalled:  func(m *mockAttachmentService) bool { return m.attachCalled },
			calledName: "Attach",
		},
		{
			name:       "detach entry attachment",
			method:     http.MethodDelete,
			path:       "/api/v1/entries/entry-123/attachments/att_123?project_id=test-project&role=inline",
			wasCalled:  func(m *mockAttachmentService) bool { return m.detachCalled },
			calledName: "Detach",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments := &mockAttachmentService{}
			brain := &mockBrainService{
				recallFunc: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
					t.Fatalf("entry wildcard HandleGetEntry was called for %s", tt.path)
					return nil, nil
				},
			}
			h := NewHandler(brain, WithAttachmentService(attachments))
			router := NewRouter(config.Config{}, WithHandler(h))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			if !tt.wasCalled(attachments) {
				t.Fatalf("%s was not called for %s %s; status = %d", tt.calledName, tt.method, tt.path, rec.Code)
			}
			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("entry attachment route was not registered, status = %d", rec.Code)
			}
		})
	}
}

func TestListEntryAttachmentsUsesProjectIDAndEntryID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entries/entry-123/attachments?project_id=test-project", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.listCalled || attachments.projectID != "test-project" || attachments.entryID != "entry-123" {
		t.Fatalf("ListForEntry called = %v, projectID = %q, entryID = %q", attachments.listCalled, attachments.projectID, attachments.entryID)
	}
	var resp types.AttachEntryAttachmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Attachments) != 1 || resp.Attachments[0].ID != "att_123" || resp.Attachments[0].Role != "inline" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAttachEntryAttachmentParsesJSONBodyAndProjectID(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)
	body := strings.NewReader(`{"attachment":{"id":"att_123","role":"inline","caption":"diagram"}}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entries/entry-123/attachments?project_id=test-project", body)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.attachCalled || attachments.projectID != "test-project" || attachments.entryID != "entry-123" {
		t.Fatalf("Attach called = %v, projectID = %q, entryID = %q", attachments.attachCalled, attachments.projectID, attachments.entryID)
	}
	if attachments.attachReq.Attachment.ID != "att_123" || attachments.attachReq.Attachment.Role != "inline" || attachments.attachReq.Attachment.Caption != "diagram" {
		t.Fatalf("attachReq = %#v", attachments.attachReq)
	}
}

func TestAttachEntryAttachmentAcceptsProjectIDInJSONBody(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)
	body := strings.NewReader(`{"project_id":"test-project","attachment":{"id":"att_123","role":"inline"}}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entries/entry-123/attachments", body)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if attachments.projectID != "test-project" {
		t.Fatalf("projectID = %q", attachments.projectID)
	}
}

func TestDetachEntryAttachmentUsesProjectIDAttachmentIDAndRole(t *testing.T) {
	attachments := &mockAttachmentService{}
	router := newAttachmentTestRouter(attachments)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/entries/entry-123/attachments/att_123?project_id=test-project&role=inline", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !attachments.detachCalled || attachments.projectID != "test-project" || attachments.entryID != "entry-123" || attachments.attachmentID != "att_123" || attachments.role != "inline" {
		t.Fatalf("Detach called = %v, projectID = %q, entryID = %q, attachmentID = %q, role = %q", attachments.detachCalled, attachments.projectID, attachments.entryID, attachments.attachmentID, attachments.role)
	}
}

func TestEntryAttachmentEndpointsValidateProjectIDAndBody(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list missing project", method: http.MethodGet, path: "/api/v1/entries/entry-123/attachments"},
		{name: "attach missing project", method: http.MethodPost, path: "/api/v1/entries/entry-123/attachments", body: `{"attachment":{"id":"att_123","role":"inline"}}`},
		{name: "attach invalid json", method: http.MethodPost, path: "/api/v1/entries/entry-123/attachments?project_id=test-project", body: `{`},
		{name: "detach missing project", method: http.MethodDelete, path: "/api/v1/entries/entry-123/attachments/att_123?role=inline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments := &mockAttachmentService{}
			router := newAttachmentTestRouter(attachments)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEntryAttachmentEndpointsMapServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		configure  func(*mockAttachmentService)
		wantStatus int
	}{
		{name: "list not found", method: http.MethodGet, path: "/api/v1/entries/missing/attachments?project_id=test-project", configure: func(m *mockAttachmentService) { m.listForEntryErr = ErrNotFound }, wantStatus: http.StatusNotFound},
		{name: "attach bad request", method: http.MethodPost, path: "/api/v1/entries/entry-123/attachments?project_id=test-project", body: `{"attachment":{"id":"att_123","role":"inline"}}`, configure: func(m *mockAttachmentService) { m.attachErr = errors.New("attachment role is required") }, wantStatus: http.StatusBadRequest},
		{name: "detach not found", method: http.MethodDelete, path: "/api/v1/entries/entry-123/attachments/att_123?project_id=test-project&role=inline", configure: func(m *mockAttachmentService) { m.detachErr = ErrNotFound }, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments := &mockAttachmentService{}
			tt.configure(attachments)
			router := newAttachmentTestRouter(attachments)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
