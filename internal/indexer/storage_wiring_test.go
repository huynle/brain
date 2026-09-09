package indexer

import (
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestIndexerRetainsTenantView(t *testing.T) {
	view := newTestStorage(t)
	idx := NewIndexer(t.TempDir(), view)
	if idx.storage != view || idx.storage.TenantID() != tenant.Local {
		t.Fatal("indexer lost tenant view")
	}
}
