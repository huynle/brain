package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

func TestTenantRootsMigrationFrom27(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("DROP TABLE IF EXISTS tenant_roots; DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES (27)"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("INSERT INTO feature_pause_state(project_id,feature_id,paused,updated_at) VALUES ('legacy-project','legacy-feature',1,'2026-09-06T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	// Exercise the migration itself: InitSchema also creates missing tables and
	// would otherwise mask a missing v28 migration branch.
	if err = migrateSchema(s.DB()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("SELECT tenant_id FROM tenant_roots"); err != nil {
		t.Fatalf("v27 migration did not create tenant_roots: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err = s.DB().QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tenant_roots'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("migration did not create durable tenant_roots table")
	}
	if version, err := GetSchemaVersion(s.DB()); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("upgraded version = %d, err %v", version, err)
	}
	ctx := context.Background()
	base := filepath.Join(filepath.Dir(path), "brain")
	r, err := tenantfs.New(s, base)
	if err != nil {
		t.Fatal(err)
	}
	local, err := r.ProvisionLocal(ctx, base+"//./", filepath.Join(filepath.Dir(path), "independent-cas"))
	if err != nil {
		t.Fatal(err)
	}
	want := []tenantfs.Mapping{local}
	for restart := 0; restart < 3; restart++ {
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = New(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		r, err = tenantfs.New(s, base)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range want {
			if got, err := r.Lookup(ctx, m.ID); err != nil || got != m {
				t.Fatalf("restart %d lost mapping: %+v, %v; want %+v", restart, got, err, m)
			}
		}
		if restart == 1 { // second single-mode restart, then promotion
			m, err := r.Provision(ctx, tenant.MustParse("tenant-a"), tenantfs.Overrides{})
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, m)
		}
		var paused int
		if err := s.DB().QueryRow("SELECT paused FROM feature_pause_state WHERE project_id='legacy-project' AND feature_id='legacy-feature'").Scan(&paused); err != nil || paused != 1 {
			t.Fatalf("legacy data lost on restart %d: paused %d, err %v", restart, paused, err)
		}
	}
}
