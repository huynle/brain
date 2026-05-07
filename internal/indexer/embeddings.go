package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/embeddings"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/pkg/markdown"
)

// BackfillMissingOrStaleEmbeddings refreshes embeddings for indexed notes that
// are missing an embedding for the configured model or have stale semantic text.
func (idx *Indexer) BackfillMissingOrStaleEmbeddings(ctx context.Context) (int, error) {
	if idx.embedder == nil || idx.embeddingModel == "" {
		return 0, nil
	}

	candidates, err := idx.storage.FindMissingOrStaleEntryEmbeddings(ctx, idx.embeddingModel, 0)
	if err != nil {
		return 0, err
	}

	var backfilled int
	var errs []error
	for _, candidate := range candidates {
		pf, err := markdown.ParseFile(candidate.Path, idx.brainDir)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse backfill candidate %q: %w", candidate.Path, err))
			continue
		}
		if err := idx.indexEmbeddings(ctx, pf); err != nil {
			if isEmbeddingProviderError(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("backfill embedding for %q: %w", candidate.Path, err))
			continue
		}
		backfilled++
	}
	return backfilled, errors.Join(errs...)
}

func (idx *Indexer) indexEmbeddings(ctx context.Context, pf *markdown.ParsedFile) error {
	if idx.embedder == nil || idx.embeddingModel == "" {
		return nil
	}

	text := strings.TrimSpace(strings.Join([]string{pf.Title, pf.Lead, pf.Body}, "\n\n"))
	if text == "" {
		_, err := idx.storage.DeleteEntryEmbeddings(ctx, pf.Path, idx.embeddingModel)
		return err
	}
	contentHash := storage.SemanticEmbeddingContentHash(pf.Title, pf.Lead, pf.Body)
	existing, err := idx.storage.GetEntryEmbedding(ctx, pf.Path, 0, idx.embeddingModel)
	if err != nil {
		return err
	}
	if existingEmbeddingIsCurrent(existing, contentHash) {
		_, err := idx.storage.DeleteStaleEntryEmbeddingChunks(ctx, pf.Path, idx.embeddingModel, []int{0})
		return err
	}

	vectors, err := idx.embedder.Embed(ctx, []string{text})
	if err != nil {
		return err
	}
	if len(vectors) != 1 {
		return fmt.Errorf("embedder returned %d vectors for 1 input", len(vectors))
	}
	if len(vectors[0]) == 0 {
		return fmt.Errorf("embedder returned empty vector")
	}

	if err := idx.storage.UpsertEntryEmbeddings(ctx, []*storage.EntryEmbeddingRow{{
		Path:        pf.Path,
		ChunkIndex:  0,
		ContentHash: contentHash,
		Model:       idx.embeddingModel,
		Dimensions:  len(vectors[0]),
		Embedding:   embeddings.EncodeFloat32Vector(vectors[0]),
	}}); err != nil {
		return err
	}
	_, err = idx.storage.DeleteStaleEntryEmbeddingChunks(ctx, pf.Path, idx.embeddingModel, []int{0})
	return err
}

func existingEmbeddingIsCurrent(existing *storage.EntryEmbeddingRow, contentHash string) bool {
	if existing == nil || existing.ContentHash != contentHash || existing.Dimensions <= 0 {
		return false
	}
	vector, err := embeddings.DecodeFloat32Vector(existing.Embedding)
	return err == nil && len(vector) == existing.Dimensions
}

func isEmbeddingProviderError(err error) bool {
	var providerErr *embeddings.ProviderError
	return errors.As(err, &providerErr)
}
