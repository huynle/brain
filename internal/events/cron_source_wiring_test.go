package events

import (
	"testing"

	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestScheduleSourceRetainsTenantView(t *testing.T) {
	view, err := storagetest.New(t, t.TempDir()+"/brain.db")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStorageScheduleSource(view)
	if s.store != view || s.store.TenantID() != tenant.Local {
		t.Fatal("schedule source lost tenant view")
	}
}
