package blobstore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

func TestTenantBlobLayoutAndExclusions(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	localBlobs := t.TempDir()
	repo, err := storage.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	r, err := tenantfs.New(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(ctx, base, localBlobs); err != nil {
		t.Fatal(err)
	}
	ids := []tenant.ID{tenant.Local, tenant.MustParse("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), tenant.MustParse("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}
	for _, id := range ids[1:] {
		if _, err := r.Provision(ctx, id, tenantfs.Overrides{}); err != nil {
			t.Fatal(err)
		}
	}
	stores := map[tenant.ID]*FilesystemStore{}
	var digest string
	for _, id := range ids {
		s, err := NewTenantFilesystemStore(r, id, 1024)
		if err != nil {
			t.Fatal(err)
		}
		stores[id] = s
		hash, _, err := s.Put(strings.NewReader("same bytes"))
		if err != nil {
			t.Fatal(err)
		}
		digest = hash
		expected, err := r.BlobPath(ctx, id, hash)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(expected); err != nil {
			t.Errorf("%s blob not at persisted layout %s: %v", id, expected, err)
		}
		f, err := s.Get(hash)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(b) != "same bytes" {
			t.Fatalf("read: %q %v", b, err)
		}
	}
	if err := stores[ids[1]].Delete(digest); err != nil {
		t.Fatal(err)
	}
	f, err := stores[ids[2]].Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Local shard aliases must not point into reserved foreign storage.
	if err := stores[tenant.Local].Delete(digest); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(localBlobs, digest[:2], digest[2:4])
	if err := os.Remove(shard); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(localBlobs, "tenants/foreign")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, digest), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, shard); err != nil {
		t.Fatal(err)
	}
	if f, err := stores[tenant.Local].Get(digest); err == nil {
		f.Close()
		t.Error("get followed foreign shard alias")
	}
	if _, _, err := stores[tenant.Local].Put(strings.NewReader("same bytes")); err == nil {
		t.Error("put admitted foreign shard alias")
	}
	if err := stores[tenant.Local].Delete(digest); err == nil {
		t.Error("delete admitted foreign shard alias")
	}
	if _, err := os.Stat(filepath.Join(target, digest)); err != nil {
		t.Errorf("foreign blob changed: %v", err)
	}
	staging := filepath.Join(localBlobs, ".staging")
	if err := os.Remove(staging); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, staging); err != nil {
		t.Fatal(err)
	}
	if _, _, err := stores[tenant.Local].Put(strings.NewReader("new content")); err == nil {
		t.Error("staging admitted foreign alias")
	}
	files, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Error("staging created a foreign file")
	}
}

func TestLegacyBlobIORetainsAnchorsAcrossRestartAndPromotion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, later := filepath.Join(dir, "first"), filepath.Join(dir, "later")
	for _, path := range []string{first, later} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(first)
	brain, blobs := "brain//./", "independent-cas/./"
	legacy, err := NewFilesystemStore(blobs, 1024)
	if err != nil {
		t.Fatal(err)
	}
	digest, _, err := legacy.Put(strings.NewReader("legacy bytes"))
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "shared.db")
	var original tenantfs.Mapping
	for restart := 0; restart < 3; restart++ {
		repo, err := storage.New(db)
		if err != nil {
			t.Fatal(err)
		}
		r, err := tenantfs.New(repo, filepath.Join(first, "brain"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := r.ProvisionLocal(ctx, brain, blobs)
		if err != nil {
			t.Fatal(err)
		}
		if restart == 0 {
			original = m
		} else if m != original {
			t.Fatalf("mapping changed after restart: %+v", m)
		}
		if restart == 1 {
			if _, err := r.Provision(ctx, tenant.MustParse("promoted"), tenantfs.Overrides{}); err != nil {
				t.Fatal(err)
			}
		}
		bound, err := NewTenantFilesystemStore(r, tenant.Local, 1024)
		if err != nil {
			t.Fatal(err)
		}
		f, err := bound.Get(digest)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(body) != "legacy bytes" {
			t.Fatalf("restart read %q: %v", body, err)
		}
		if got, _, err := bound.Put(strings.NewReader("legacy bytes")); err != nil || got != digest {
			t.Fatalf("restart put %s: %v", got, err)
		}
		if err := repo.Close(); err != nil {
			t.Fatal(err)
		}
		t.Chdir(later)
	}
	if _, err := os.Stat(filepath.Join(later, "independent-cas")); !os.IsNotExist(err) {
		t.Fatalf("I/O reinterpreted relative CAS against new cwd: %v", err)
	}
	if original.BrainRoot != brain || original.BlobRoot != blobs {
		t.Fatalf("lexical config lost: %+v", original)
	}
}
