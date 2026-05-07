package indexer

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/embeddings"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/pkg/markdown"
)

func (idx *Indexer) indexEmbeddings(ctx context.Context, pf *markdown.ParsedFile) error {
	if idx.embedder == nil || idx.embeddingModel == "" {
		return nil
	}

	text := strings.TrimSpace(strings.Join([]string{pf.Title, pf.Lead, pf.Body}, "\n\n"))
	if text == "" {
		_, err := idx.storage.DeleteEntryEmbeddings(ctx, pf.Path, idx.embeddingModel)
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
		ContentHash: pf.Checksum,
		Model:       idx.embeddingModel,
		Dimensions:  len(vectors[0]),
		Embedding:   embeddings.EncodeFloat32Vector(vectors[0]),
	}}); err != nil {
		return err
	}
	_, err = idx.storage.DeleteStaleEntryEmbeddingChunks(ctx, pf.Path, idx.embeddingModel, []int{0})
	return err
}
