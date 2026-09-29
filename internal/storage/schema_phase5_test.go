package storage

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestPhase5SingleRuntimeAdmission(t *testing.T) {
	for _, source := range provenanceSources {
		t.Run(source.profile+source.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			owner, err := NewWithDB(db)
			if err != nil {
				t.Fatal("genuine single-mode source refused:", err)
			}
			v, err := GetSchemaVersion(db)
			if err != nil || v != 30 {
				t.Fatalf("runtime = %d: %v", v, err)
			}
			local, err := owner.ForTenant(tenant.Local)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = local.ReadEntryChanges(tenant.Into(context.Background(), tenant.Local), "", 0, 10); err != nil {
				t.Fatal(err)
			}
			if _, err = local.GetNoteByPath(context.Background(), "absent"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
