package storage

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestForTenantBinding(t *testing.T) {
	s := &StorageLayer{} // No database: binding must never execute SQL.
	invalid, err := s.ForTenant(tenant.ID{})
	if invalid != nil || err == nil {
		t.Fatal("zero tenant must return nil handle and error before SQL")
	}
	for _, id := range []tenant.ID{tenant.Local, tenant.MustParse("acme")} {
		result, err := s.ForTenant(id)
		if err != nil || result == nil {
			t.Fatalf("valid tenant %s must bind without SQL", id)
		}
		got := result.TenantID()
		if got != id {
			t.Fatalf("bound tenant = %v, want %v", got, id)
		}
		if result.StorageLayer != s {
			t.Fatal("tenant handle must retain the same StorageLayer")
		}
	}
}

func TestTenantListQueryRejectsInvalidHandlesBeforeSQL(t *testing.T) {
	for name, handle := range map[string]*TenantStore{
		"nil":              nil,
		"zero":             {},
		"zero ID":          {StorageLayer: &StorageLayer{}, tenantID: tenant.ID{}},
		"nil storage":      {tenantID: tenant.Local},
		"non-local pre-P4": {StorageLayer: &StorageLayer{}, tenantID: tenant.MustParse("acme")},
	} {
		t.Run(name, func(t *testing.T) {
			for _, opts := range []*ListOptions{nil, {}, {ProjectID: "project"}} {
				query, args, err := handle.listQuery(opts)
				if err == nil || query != "" || args != nil {
					t.Fatalf("invalid query must return no SQL/args and an error: %q %v %v", query, args, err)
				}
				notes, err := handle.ListNotes(context.Background(), opts)
				if err == nil || notes != nil {
					t.Fatalf("invalid handle must reject before SQL: %v %v", notes, err)
				}
			}
		})
	}
}

func TestTenantListQueryLocalCompatibility(t *testing.T) {
	s := newTestStorage(t)
	seedListNotes(t, s)
	handle, err := s.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []*ListOptions{nil, {}, {ProjectID: "alpha", Limit: 1}} {
		query, _, err := handle.listQuery(opts)
		if err != nil || query == "" || strings.Contains(query, "tenant_id") {
			t.Fatalf("local query must use the pre-P4 schema: %q %v", query, err)
		}
		want, err := s.ListNotes(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		got, err := handle.ListNotes(context.Background(), opts)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("local list = %v, %v; want %v", got, err, want)
		}
	}
}

func FuzzTenantQueryInvalidHandles(f *testing.F) {
	for _, raw := range []string{"", "../escape", "UPPER", "a b", "global", "local", "acme", strings.Repeat("a", 41), "\x00"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		id, parseErr := tenant.Parse(raw)
		for _, store := range []*StorageLayer{nil, {}} { // Deliberately no database.
			handle, err := store.ForTenant(id)
			if parseErr != nil || store == nil {
				if err == nil || handle != nil {
					t.Fatalf("invalid binding accepted: %q", raw)
				}
			} else if err != nil || handle.TenantID() != id {
				t.Fatalf("valid binding rejected: %q: %v", raw, err)
			}
			// Even a handle constructed inside storage must fail closed. Parsed
			// non-local IDs are not executable against the pre-P4 content schema.
			forged := &TenantStore{StorageLayer: store, tenantID: id}
			query, args, err := forged.listQuery(nil)
			if err == nil || query != "" || args != nil {
				t.Fatalf("query accepted before SQL: %q", raw)
			}
			notes, err := forged.ListNotes(context.Background(), nil)
			if err == nil || notes != nil {
				t.Fatalf("query executed for invalid/pre-P4 handle: %q", raw)
			}
		}
	})
}
