package embeddings

import "context"

// Embedder produces one embedding vector for each input text, preserving order.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
