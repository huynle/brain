package service

import (
	"context"

	"github.com/huynle/brain-api/internal/types"
)

// AttachmentExtractor derives searchable text from attachment content.
// Implementations must never log raw attachment bytes or encoded payloads.
type AttachmentExtractor interface {
	Extract(ctx context.Context, req types.AttachmentExtractionRequest) (types.AttachmentExtractionResponse, error)
}
