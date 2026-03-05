package api

import (
	"context"
	"errors"

	"github.com/huynle/brain-api/internal/types"
)

// Sentinel errors returned by BrainService implementations.
var (
	ErrNotFound = errors.New("not found")
)

// BrainService defines the interface for brain entry operations.
// Implementations handle persistence; handlers handle HTTP concerns.
type BrainService interface {
	// Save creates a new brain entry.
	Save(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)

	// Recall retrieves a brain entry by path or 8-char ID.
	Recall(ctx context.Context, pathOrID string) (*types.BrainEntry, error)

	// Update modifies an existing brain entry.
	Update(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)

	// Delete removes a brain entry by path or ID.
	Delete(ctx context.Context, pathOrID string) error

	// List returns entries matching the given filters.
	List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)

	// Move moves an entry to a different project.
	Move(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error)
}
