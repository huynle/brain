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
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

type mockAttachmentService struct {
	getCalled     bool
	listCalled    bool
	attachCalled  bool
	detachCalled  bool
	deleteCalled  bool
	createCalled  bool
	openCalled    bool
	textCalled    bool
	extractCalled bool

	projectID    string
	entryID      string
	attachmentID string
	role         string
	createReq    types.CreateAttachmentRequest
	attachReq    types.AttachEntryAttachmentRequest
	extractReq   types.AttachmentExtractionRequest
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
	extractResult   *types.AttachmentExtractionResult
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
