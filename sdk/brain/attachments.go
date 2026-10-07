package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
)

type rawBody struct {
	data        []byte
	contentType string
}

func (s AttachmentsService) List(ctx context.Context, project string) (*ListAttachmentsResponse, error) {
	return result[ListAttachmentsResponse](s.c, ctx, "GET", "/attachments", nil, url.Values{"project_id": {project}}, RequestOptions{})
}
func (s AttachmentsService) Get(ctx context.Context, project, id string) (*Attachment, error) {
	return result[Attachment](s.c, ctx, "GET", "/attachments/"+url.PathEscape(id), nil, url.Values{"project_id": {project}}, RequestOptions{})
}
func (s AttachmentsService) Delete(ctx context.Context, project, id string, opts RequestOptions) (*AttachmentDeletionResponse, error) {
	return result[AttachmentDeletionResponse](s.c, ctx, "DELETE", "/attachments/"+url.PathEscape(id), nil, url.Values{"project_id": {project}}, opts)
}
func (s AttachmentsService) Extract(ctx context.Context, project, id string, r AttachmentExtractionRequest, opts RequestOptions) (*AttachmentExtractionResult, error) {
	return result[AttachmentExtractionResult](s.c, ctx, "POST", "/attachments/"+url.PathEscape(id)+"/extract", r, url.Values{"project_id": {project}}, opts)
}
func (s AttachmentsService) Text(ctx context.Context, project, id string) (string, error) {
	var data []byte
	err := s.c.request(ctx, "GET", "/attachments/"+url.PathEscape(id)+"/text", nil, url.Values{"project_id": {project}}, RequestOptions{}, &data)
	return string(data), err
}
func (s AttachmentsService) ForEntry(ctx context.Context, project, id string) (*AttachEntryAttachmentResponse, error) {
	return result[AttachEntryAttachmentResponse](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/attachments", nil, url.Values{"project_id": {project}}, RequestOptions{})
}
func (s AttachmentsService) Attach(ctx context.Context, project, id string, r AttachEntryAttachmentRequest, opts RequestOptions) (*AttachEntryAttachmentResponse, error) {
	return result[AttachEntryAttachmentResponse](s.c, ctx, "POST", "/entries/"+url.PathEscape(id)+"/attachments", r, url.Values{"project_id": {project}}, opts)
}
func (s AttachmentsService) Detach(ctx context.Context, project, id, attachmentID, role string, opts RequestOptions) (*AttachEntryAttachmentResponse, error) {
	return result[AttachEntryAttachmentResponse](s.c, ctx, "DELETE", "/entries/"+url.PathEscape(id)+"/attachments/"+url.PathEscape(attachmentID), nil, url.Values{"project_id": {project}, "role": {role}}, opts)
}

type AttachmentsService struct{ c *Client }
type UploadRequest struct {
	Filename, ContentType string
	Content               []byte
	Metadata              map[string]string
}

func (c *Client) Attachments() AttachmentsService { return AttachmentsService{c} }
func (s AttachmentsService) Upload(ctx context.Context, project string, r UploadRequest, opts RequestOptions) (*CreateAttachmentResponse, error) {
	if r.Filename == "" || r.Filename == "." || r.Filename == ".." || strings.ContainsAny(r.Filename, "/\\\r\n\x00") || strings.ContainsAny(r.ContentType, "\r\n\x00") {
		return nil, &Error{Code: "invalid_request"}
	}
	if int64(len(r.Content)) > s.c.limit {
		return nil, &Error{Code: "request_too_large"}
	}
	metadata, err := json.Marshal(r.Metadata)
	if err != nil {
		return nil, &Error{Code: "invalid_request"}
	}
	if int64(len(metadata))+int64(len(project))+int64(len(r.Filename))+int64(len(r.ContentType)) > s.c.limit {
		return nil, &Error{Code: "request_too_large"}
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if err := w.WriteField("project_id", project); err != nil {
		return nil, err
	}
	if r.Metadata != nil {
		if err := w.WriteField("metadata", string(metadata)); err != nil {
			return nil, err
		}
	}
	contentType := r.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := textproto.MIMEHeader{"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": r.Filename})}, "Content-Type": {contentType}}
	part, err := w.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(r.Content); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	if int64(b.Len()) > s.c.limit {
		return nil, &Error{Code: "request_too_large"}
	}
	return result[CreateAttachmentResponse](s.c, ctx, "POST", "/attachments", rawBody{b.Bytes(), w.FormDataContentType()}, nil, opts)
}
func (s AttachmentsService) Download(ctx context.Context, project, id string) ([]byte, error) {
	var data []byte
	err := s.c.request(ctx, "GET", "/attachments/"+url.PathEscape(id)+"/content", nil, url.Values{"project_id": {project}}, RequestOptions{}, &data)
	return data, err
}
