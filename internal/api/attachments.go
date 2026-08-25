package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/types"
)

const maxAttachmentUploadMemory = 32 << 20

// HandleCreateAttachment handles POST /attachments.
func (h *Handler) HandleCreateAttachment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxAttachmentUploadMemory); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "request must be multipart/form-data")
		return
	}

	projectID := strings.TrimSpace(r.FormValue("project_id"))
	if projectID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "project_id is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "multipart file field is required")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "failed to read uploaded file")
		return
	}

	metadata, err := parseAttachmentMetadata(r.FormValue("metadata"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(content)
	}

	req := types.CreateAttachmentRequest{
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		Size:        int64(len(content)),
		Metadata:    metadata,
	}

	resp, err := h.attachments.Create(r.Context(), projectID, req, bytes.NewReader(content))
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, resp)
}

// HandleListAttachments handles GET /attachments?project_id=...
func (h *Handler) HandleListAttachments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	resp, err := h.attachments.List(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetAttachment handles GET /attachments/{attachmentID}?project_id=...
func (h *Handler) HandleGetAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	attachment, err := h.attachments.Get(r.Context(), projectID, chi.URLParam(r, "attachmentID"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, attachment)
}

// HandleDeleteAttachment handles DELETE /attachments/{attachmentID}?project_id=...
func (h *Handler) HandleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	deleted, err := h.attachments.Delete(r.Context(), projectID, chi.URLParam(r, "attachmentID"))
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	if !deleted {
		WriteError(w, http.StatusConflict, "Conflict", "attachment is still referenced by one or more entries")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

// HandleExtractAttachment handles POST /attachments/{attachmentID}/extract?project_id=...
func (h *Handler) HandleExtractAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}

	var req types.AttachmentExtractionRequest
	if r.Body != nil && r.Body != http.NoBody {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			WriteError(w, http.StatusBadRequest, "Bad Request", "request body must be JSON compatible with AttachmentExtractionRequest")
			return
		}
	}

	result, err := h.attachments.ExtractAttachmentText(r.Context(), projectID, chi.URLParam(r, "attachmentID"), req)
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, result)
}

// HandleBackfillAttachmentExtraction handles POST /attachments/backfill/extraction?project_id=...
func (h *Handler) HandleBackfillAttachmentExtraction(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}

	var req types.AttachmentExtractionBackfillRequest
	if r.Body != nil && r.Body != http.NoBody {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			WriteError(w, http.StatusBadRequest, "Bad Request", "request body must be JSON compatible with AttachmentExtractionBackfillRequest")
			return
		}
	}

	result, err := h.attachments.BackfillAttachmentExtraction(r.Context(), projectID, req)
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, result)
}

// HandleDownloadAttachment handles GET /attachments/{attachmentID}/content?project_id=...
func (h *Handler) HandleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	attachment, content, err := h.attachments.Open(r.Context(), projectID, chi.URLParam(r, "attachmentID"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	defer content.Close()
	contentType := "application/octet-stream"
	if attachment != nil && attachment.ContentType != "" {
		contentType = attachment.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	if attachment != nil && attachment.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(attachment.Size, 10))
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if attachment != nil && attachment.Filename != "" {
		w.Header().Set("Content-Disposition", safeAttachmentContentDisposition(attachment.Filename))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

func attachmentProjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "project_id is required")
		return "", false
	}
	return projectID, true
}

func parseAttachmentMetadata(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, fmt.Errorf("metadata must be a JSON object with string values")
	}
	return metadata, nil
}

func safeAttachmentContentDisposition(filename string) string {
	safe := sanitizeAttachmentFilename(filename)
	return "attachment; filename=" + strconv.Quote(safe)
}

func sanitizeAttachmentFilename(filename string) string {
	base := filepath.Base(filename)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "download"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	result := strings.Trim(b.String(), " .")
	if result == "" {
		return "download"
	}
	return result
}

type attachmentTextOpener interface {
	OpenText(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error)
}

// HandleGetAttachmentText handles GET /attachments/{attachmentID}/text?project_id=...
func (h *Handler) HandleGetAttachmentText(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	textOpener, ok := h.attachments.(attachmentTextOpener)
	if !ok {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Attachment text endpoint not implemented")
		return
	}
	attachment, content, err := textOpener.OpenText(r.Context(), projectID, chi.URLParam(r, "attachmentID"))
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	defer content.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if attachment != nil && attachment.Filename != "" {
		w.Header().Set("Content-Disposition", safeInlineAttachmentContentDisposition(attachment.Filename))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

func safeInlineAttachmentContentDisposition(filename string) string {
	safe := sanitizeAttachmentFilename(filename)
	return "inline; filename=" + strconv.Quote(safe)
}

// HandleListEntryAttachments handles GET /entries/{id}/attachments.
func (h *Handler) HandleListEntryAttachments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	resp, err := h.attachments.ListForEntry(r.Context(), projectID, entryPathParam(r, "id"))
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleAttachEntryAttachment handles POST /entries/{id}/attachments.
func (h *Handler) HandleAttachEntryAttachment(w http.ResponseWriter, r *http.Request) {
	req, projectID, err := parseAttachEntryAttachmentRequest(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	resp, err := h.attachments.Attach(r.Context(), projectID, entryPathParam(r, "id"), req)
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleDetachEntryAttachment handles DELETE /entries/{id}/attachments/{attachmentID}.
func (h *Handler) HandleDetachEntryAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := attachmentProjectID(w, r)
	if !ok {
		return
	}
	resp, err := h.attachments.Detach(r.Context(), projectID, entryPathParam(r, "id"), entryPathParam(r, "attachmentID"), r.URL.Query().Get("role"))
	if err != nil {
		writeAttachmentServiceError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

func parseAttachEntryAttachmentRequest(r *http.Request) (types.AttachEntryAttachmentRequest, string, error) {
	var body struct {
		types.AttachEntryAttachmentRequest
		ProjectID string `json:"project_id"`
		Project   string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return types.AttachEntryAttachmentRequest{}, "", fmt.Errorf("request body must be JSON compatible with AttachEntryAttachmentRequest")
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		projectID = strings.TrimSpace(body.ProjectID)
	}
	if projectID == "" {
		projectID = strings.TrimSpace(body.Project)
	}
	if projectID == "" {
		return types.AttachEntryAttachmentRequest{}, "", fmt.Errorf("project_id is required")
	}
	return body.AttachEntryAttachmentRequest, projectID, nil
}

func writeAttachmentServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		WriteError(w, http.StatusNotFound, "Not Found", err.Error())
		return
	}
	if errors.Is(err, blobstore.ErrTooLarge) {
		WriteError(w, http.StatusRequestEntityTooLarge, "Request Entity Too Large", err.Error())
		return
	}
	if isAttachmentUnsupportedMediaTypeError(err) {
		WriteError(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", err.Error())
		return
	}
	if isAttachmentBadRequestError(err) {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
}

func isAttachmentBadRequestError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "required") || strings.Contains(msg, "unsafe") || strings.Contains(msg, "must") || strings.Contains(msg, "invalid")
}

func isAttachmentUnsupportedMediaTypeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "mime type") && (strings.Contains(msg, "blocked") || strings.Contains(msg, "not allowed"))
}
