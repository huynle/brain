package api

import (
	"context"
	"io"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

type mockAttachmentService struct{}

func (m *mockAttachmentService) Create(ctx context.Context, projectID string, req types.CreateAttachmentRequest, content io.Reader) (*types.CreateAttachmentResponse, error) {
	return nil, nil
}

func (m *mockAttachmentService) Get(ctx context.Context, projectID, attachmentID string) (*types.Attachment, error) {
	return nil, nil
}

func (m *mockAttachmentService) Open(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error) {
	return nil, nil, nil
}

func (m *mockAttachmentService) List(ctx context.Context, projectID string) (*types.ListAttachmentsResponse, error) {
	return nil, nil
}

func (m *mockAttachmentService) Attach(ctx context.Context, projectID, pathOrID string, req types.AttachEntryAttachmentRequest) (*types.AttachEntryAttachmentResponse, error) {
	return nil, nil
}

func (m *mockAttachmentService) Detach(ctx context.Context, projectID, pathOrID, attachmentID, role string) (*types.AttachEntryAttachmentResponse, error) {
	return nil, nil
}

func (m *mockAttachmentService) Delete(ctx context.Context, projectID, attachmentID string) (bool, error) {
	return false, nil
}

func TestWithAttachmentServiceSetsHandlerDependency(t *testing.T) {
	attachments := &mockAttachmentService{}
	h := NewHandler(&mockBrainService{}, WithAttachmentService(attachments))
	if h.attachments != attachments {
		t.Fatal("WithAttachmentService did not set handler attachment service")
	}
}
