package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantAttachmentsIsolation(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	if _, err := owner.db.Exec("DELETE FROM entry_attachments"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("DELETE FROM attachments"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("same bytes")))
	create := func(s *TenantStore) *AttachmentRow {
		t.Helper()
		r, err := s.CreateAttachment(ctx, AttachmentInput{Digest: digest, Size: 10})
		if err != nil || r == nil {
			t.Fatalf("create: %+v %v", r, err)
		}
		return r
	}
	aa, bb := create(a), create(b)
	if aa.ID == bb.ID {
		t.Fatal("cross-tenant digest dedup")
	}
	if bb.ID != aa.ID+1 {
		t.Fatalf("expected sequential foreign IDs: %d %d", aa.ID, bb.ID)
	}
	if create(a).ID != aa.ID || create(b).ID != bb.ID {
		t.Fatal("owner dedup failed")
	}
	for i, s := range []*TenantStore{a, b} {
		own, foreign := aa, bb
		if i == 1 {
			own, foreign = bb, aa
		}
		for _, project := range []string{"one", "two"} {
			path := "projects/" + project + "/note/same.md"
			if _, err := s.InsertNote(ctx, sampleNote(path, "same0001", project)); err != nil {
				t.Fatal(err)
			}
			if err := s.LinkAttachmentToEntry(ctx, path, own.ID, "source"); err != nil {
				t.Fatal(err)
			}
			if err := s.LinkAttachmentToEntry(ctx, path, own.ID, "source"); err != nil {
				t.Fatal(err)
			}
			if err := s.LinkAttachmentToEntry(ctx, path, foreign.ID, "source"); err == nil {
				t.Fatal("foreign parent accepted")
			}
			if ok, err := s.UnlinkAttachmentFromEntry(ctx, path, foreign.ID, "source"); err != nil || ok {
				t.Fatalf("foreign unlink: %v %v", ok, err)
			}
			rows, err := s.ListAttachmentsForEntry(ctx, path)
			if err != nil || len(rows) != 1 || rows[0].ID != own.ID {
				t.Fatalf("entry list: %+v %v", rows, err)
			}
		}
		if r, err := s.GetAttachment(ctx, foreign.ID); err != nil || r != nil {
			t.Fatalf("foreign read: %+v %v", r, err)
		}
		if r, err := s.GetAttachmentByDigest(ctx, digest); err != nil || r == nil || r.ID != own.ID {
			t.Fatalf("digest: %+v %v", r, err)
		}
		if rows, err := s.ListAttachments(ctx); err != nil || len(rows) != 1 || rows[0].ID != own.ID {
			t.Fatalf("list: %+v %v", rows, err)
		}
		if n, err := s.CountAttachmentReferences(ctx, own.ID); err != nil || n != 2 {
			t.Fatalf("count: %d %v", n, err)
		}
		if n, err := s.CountAttachmentReferences(ctx, foreign.ID); err != nil || n != 0 {
			t.Fatalf("foreign count: %d %v", n, err)
		}
		if rows, err := s.ListEntryReferencesForAttachment(ctx, own.ID); err != nil || len(rows) != 2 {
			t.Fatalf("refs: %+v %v", rows, err)
		}
		if rows, err := s.ListEntryReferencesForAttachment(ctx, foreign.ID); err != nil || len(rows) != 0 {
			t.Fatalf("foreign refs: %+v %v", rows, err)
		}
		for _, text := range []string{"first", s.TenantID().String()} {
			if r, err := s.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{AttachmentID: own.ID, Kind: "text", Status: "ready", Text: text}); err != nil || r == nil || r.Text != text {
				t.Fatalf("derived: %+v %v", r, err)
			}
		}
		if _, err := s.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{AttachmentID: foreign.ID, Kind: "text", Status: "ready", Text: "stolen"}); err == nil {
			t.Fatal("foreign derived accepted")
		}
		if r, err := s.GetAttachmentDerived(ctx, foreign.ID, "text"); err != nil || r != nil {
			t.Fatalf("foreign derived: %+v %v", r, err)
		}
		if rows, err := s.ListAttachmentDerived(ctx, foreign.ID); err != nil || len(rows) != 0 {
			t.Fatalf("foreign derived list: %+v %v", rows, err)
		}
		if rows, err := s.ListAttachmentDerived(ctx, own.ID); err != nil || len(rows) != 1 || rows[0].Text != s.TenantID().String() {
			t.Fatalf("derived list: %+v %v", rows, err)
		}
		if ok, err := s.DeleteAttachmentIfUnreferenced(ctx, own.ID); err != nil || ok {
			t.Fatalf("referenced delete: %v %v", ok, err)
		}
	}
	for _, project := range []string{"one", "two"} {
		if ok, err := a.UnlinkAttachmentFromEntry(ctx, "projects/"+project+"/note/same.md", aa.ID, "source"); err != nil || !ok {
			t.Fatalf("unlink: %v %v", ok, err)
		}
	}
	if ok, err := b.DeleteAttachmentIfUnreferenced(ctx, aa.ID); err != nil || ok {
		t.Fatalf("foreign delete: %v %v", ok, err)
	}
	if ok, err := a.DeleteAttachmentIfUnreferenced(ctx, aa.ID); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if rows, err := a.ListAttachmentDerived(ctx, aa.ID); err != nil || len(rows) != 0 {
		t.Fatalf("cascade: %+v %v", rows, err)
	}
	if n, err := b.CountAttachmentReferences(ctx, bb.ID); err != nil || n != 2 {
		t.Fatalf("foreign refs deleted: %d %v", n, err)
	}
	if r, err := b.GetAttachmentDerived(ctx, bb.ID, "text"); err != nil || r == nil || r.Text != "acme" {
		t.Fatalf("foreign derived changed: %+v %v", r, err)
	}
	const foreignPath = "projects/private/note/foreign.md"
	if _, err := b.InsertNote(ctx, sampleNote(foreignPath, "foreign1", "private")); err != nil {
		t.Fatal(err)
	}
	// A valid owned attachment cannot grant access to a foreign-only note path.
	replacement := create(a)
	if err := a.LinkAttachmentToEntry(ctx, foreignPath, replacement.ID, "source"); err == nil {
		t.Fatal("foreign note accepted")
	}
	if _, err := a.ListAttachmentsForEntry(ctx, foreignPath); err == nil {
		t.Fatal("foreign note listed")
	}
	if _, err := a.UnlinkAttachmentFromEntry(ctx, foreignPath, replacement.ID, "source"); err == nil {
		t.Fatal("foreign note unlinked")
	}
}

func TestTenantAttachmentsGuards(t *testing.T) {
	s := newTestStorage(t)
	foreign, err := s.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, h *TenantStore, ctx context.Context) {
		t.Helper()
		// All receiver methods must fail closed, including list/count and writes.
		if _, e := h.CreateAttachment(ctx, AttachmentInput{Digest: "bytes"}); e == nil {
			t.Error("create accepted")
		}
		if _, e := h.GetAttachment(ctx, 1); e == nil {
			t.Error("get accepted")
		}
		if _, e := h.GetAttachmentByDigest(ctx, "bytes"); e == nil {
			t.Error("digest accepted")
		}
		if _, e := h.ListAttachments(ctx); e == nil {
			t.Error("list accepted")
		}
		if e := h.LinkAttachmentToEntry(ctx, "path", 1, "source"); e == nil {
			t.Error("link accepted")
		}
		if _, e := h.UnlinkAttachmentFromEntry(ctx, "path", 1, "source"); e == nil {
			t.Error("unlink accepted")
		}
		if _, e := h.ListAttachmentsForEntry(ctx, "path"); e == nil {
			t.Error("entry list accepted")
		}
		if _, e := h.ListEntryReferencesForAttachment(ctx, 1); e == nil {
			t.Error("refs accepted")
		}
		if _, e := h.CountAttachmentReferences(ctx, 1); e == nil {
			t.Error("count accepted")
		}
		if _, e := h.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{AttachmentID: 1, Kind: "text", Status: "ready"}); e == nil {
			t.Error("upsert accepted")
		}
		if _, e := h.GetAttachmentDerived(ctx, 1, "text"); e == nil {
			t.Error("derived accepted")
		}
		if _, e := h.ListAttachmentDerived(ctx, 1); e == nil {
			t.Error("derived list accepted")
		}
		if _, e := h.DeleteAttachmentIfUnreferenced(ctx, 1); e == nil {
			t.Error("delete accepted")
		}
	}
	t.Run("v28 foreign", func(t *testing.T) { check(t, foreign, context.Background()) })
	for i, h := range []*TenantStore{nil, {}, {tenantID: tenant.Local}, {db: s.db}} {
		t.Run(fmt.Sprintf("invalid-%d", i), func(t *testing.T) { check(t, h, context.Background()) })
	}
	t.Run("nil context", func(t *testing.T) { check(t, local, nil) })
	for _, version := range []int{27, 32} {
		if _, err := s.db.Exec("UPDATE schema_version SET version=?", version); err != nil {
			t.Fatal(err)
		}
		t.Run(fmt.Sprint(version), func(t *testing.T) { check(t, local, context.Background()) })
	}
	// A claimed v29 with missing owner columns must not retry unscoped v28 SQL.
	if _, err := s.db.Exec("UPDATE schema_version SET version=29"); err != nil {
		t.Fatal(err)
	}
	t.Run("broken v29 no fallback", func(t *testing.T) { check(t, local, context.Background()) })
	if _, err := s.db.Exec("DROP TABLE schema_version"); err != nil {
		t.Fatal(err)
	}
	t.Run("missing schema", func(t *testing.T) { check(t, local, context.Background()) })
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Run("closed", func(t *testing.T) { check(t, local, context.Background()) })
}

func TestTenantAttachmentsAttachDeleteAtomic(t *testing.T) {
	for _, version := range []string{"v28", "v29"} {
		t.Run(version, func(t *testing.T) {
			s := newTestContentStorage(t)
			if version == "v29" {
				_, s, _ = migratedNoteStores(t)
			}
			ctx := context.Background()
			const path = "projects/race/note/same.md"
			if _, err := s.InsertNote(ctx, sampleNote(path, "race0001", "race")); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 40; i++ {
				att, err := s.CreateAttachment(ctx, AttachmentInput{Digest: fmt.Sprintf("race-%d", i)})
				if err != nil {
					t.Fatal(err)
				}
				start := make(chan struct{})
				var wg sync.WaitGroup
				var linkedErr, deleteErr error
				var deleted bool
				wg.Add(2)
				go func() { defer wg.Done(); <-start; linkedErr = s.LinkAttachmentToEntry(ctx, path, att.ID, "source") }()
				go func() { defer wg.Done(); <-start; deleted, deleteErr = s.DeleteAttachmentIfUnreferenced(ctx, att.ID) }()
				close(start)
				wg.Wait()
				if deleteErr != nil {
					t.Fatal(deleteErr)
				}
				row, err := s.GetAttachment(ctx, att.ID)
				if err != nil {
					t.Fatal(err)
				}
				refs, err := s.CountAttachmentReferences(ctx, att.ID)
				if err != nil {
					t.Fatal(err)
				}
				if deleted {
					if linkedErr == nil || row != nil || refs != 0 {
						t.Fatalf("delete won but link succeeded: %v %+v %d", linkedErr, row, refs)
					}
				} else if linkedErr != nil || row == nil || refs != 1 {
					t.Fatalf("link won but reference lost: %v %+v %d", linkedErr, row, refs)
				}
			}
		})
	}
}
