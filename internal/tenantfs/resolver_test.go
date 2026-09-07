package tenantfs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

var ctx = context.Background()
var a = tenant.MustParse("tenant-a")
var b = tenant.MustParse("tenant-b")

func repository(t *testing.T, s *storage.StorageLayer) tenantfs.Repository {
	t.Helper()
	repo, ok := any(s).(tenantfs.Repository)
	if !ok {
		t.Fatal("shared storage does not implement durable tenant root repository")
	}
	return repo
}
func open(t *testing.T, db, base string) (*storage.StorageLayer, *tenantfs.Resolver) {
	t.Helper()
	s, err := storage.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	r, err := tenantfs.New(repository(t, s), base)
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}
func mustLocal(t *testing.T, r *tenantfs.Resolver, brain, blob string) tenantfs.Mapping {
	t.Helper()
	m, err := r.ProvisionLocal(ctx, brain, blob)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func mustProvision(t *testing.T, r *tenantfs.Resolver, id tenant.ID, o tenantfs.Overrides) tenantfs.Mapping {
	t.Helper()
	m, err := r.Provision(ctx, id, o)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestDurablePromotion(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "brain")
	blob := filepath.Join(dir, "independent-cas") + "/"
	exact := base + "//./"
	db := filepath.Join(dir, "shared.sqlite")
	s, r := open(t, db, base)
	legacy := mustLocal(t, r, exact, blob)
	if legacy.BrainRoot != exact || legacy.BlobRoot != blob || legacy.Layout != tenantfs.LegacyCAS {
		t.Fatalf("legacy changed: %+v", legacy)
	}
	digest := strings.Repeat("a", 64)
	p, err := r.BlobPath(ctx, tenant.Local, digest)
	if err != nil || p != filepath.Join(blob, "aa", "aa", digest) {
		t.Fatalf("legacy CAS: %q %v", p, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, r = open(t, db, base) // first restart, then explicit promotion/provisioning
	if got := mustLocal(t, r, exact, blob); got != legacy {
		t.Fatalf("restart changed local: %+v", got)
	}
	ma := mustProvision(t, r, a, tenantfs.Overrides{})
	mb := mustProvision(t, r, b, tenantfs.Overrides{})
	if ma.BrainRoot != filepath.Join(base, "tenants", a.String()) || ma.BrainRoot == mb.BrainRoot || ma.BlobRoot == mb.BlobRoot {
		t.Fatalf("new roots: %+v %+v", ma, mb)
	}
	if ma.BlobRoot != filepath.Join(ma.BrainRoot, "blobs", a.String(), "sha256") {
		t.Fatalf("new blob layout: %+v", ma)
	}
	p, err = r.BlobPath(ctx, a, digest)
	if err != nil || p != filepath.Join(ma.BlobRoot, "aa", digest) {
		t.Fatalf("tenant CAS: %q %v", p, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_, r = open(t, db, base) // second restart, no boot-time schema inference
	for _, want := range []tenantfs.Mapping{legacy, ma, mb} {
		got, err := r.Lookup(ctx, want.ID)
		if err != nil || got != want {
			t.Fatalf("durable mapping: %+v %v want %+v", got, err, want)
		}
	}
	if _, err := r.ProvisionLocal(ctx, exact, blob+"different"); !errors.Is(err, tenantfs.ErrConflict) {
		t.Fatalf("changed config accepted: %v", err)
	}
	if _, err := r.Provision(ctx, a, tenantfs.Overrides{BrainRoot: filepath.Join(dir, "moved")}); !errors.Is(err, tenantfs.ErrConflict) {
		t.Fatalf("changed override accepted: %v", err)
	}
	if _, err := os.Stat(base); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolver created filesystem: %v", err)
	}
}

func TestLocalRelativeConfigRetainsPersistedAnchors(t *testing.T) {
	dir := t.TempDir()
	first, later := filepath.Join(dir, "first"), filepath.Join(dir, "later")
	for _, cwd := range []string{first, later} {
		if err := os.Mkdir(cwd, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(first)
	db := filepath.Join(dir, "shared.sqlite")
	base := filepath.Join(first, "brain")
	s, r := open(t, db, base)
	brain, blob := "brain//./", "independent-cas/./"
	want := mustLocal(t, r, brain, blob)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(later)
	// Upgrade bootstrap, second restart, promotion, and a later restart must all
	// retain the original physical locations, not reinterpret config under cwd.
	for restart := 0; restart < 3; restart++ {
		s, r = open(t, db, base)
		got, err := r.ProvisionLocal(ctx, brain, blob)
		if err != nil || got != want {
			t.Fatalf("restart %d recalculated local anchors: got %+v, err %v; want %+v", restart, got, err, want)
		}
		for _, changed := range [][2]string{{"changed", blob}, {brain, "changed"}, {"", blob}, {brain, ""}} {
			if _, err := r.ProvisionLocal(ctx, changed[0], changed[1]); !errors.Is(err, tenantfs.ErrConflict) {
				t.Fatalf("changed config accepted: %v", err)
			}
		}
		if restart == 1 {
			mustProvision(t, r, a, tenantfs.Overrides{})
		}
		p, err := r.Brain(tenant.Local).ResolveForWrite(ctx, "note.md")
		if err != nil || p != filepath.Join(first, "brain", "note.md") {
			t.Fatalf("wrong anchored path %q: %v", p, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// Retargeting the original anchor must still fail despite identical config.
	if err := os.Symlink(later, filepath.Join(first, "brain")); err != nil {
		t.Fatal(err)
	}
	_, r = open(t, db, base)
	if _, err := r.ProvisionLocal(ctx, brain, blob); !errors.Is(err, tenantfs.ErrConflict) {
		t.Fatalf("original root drift accepted: %v", err)
	}
}

func TestOwnershipAdmissions(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "brain")
	blob := filepath.Join(dir, "cas")
	_, r := open(t, filepath.Join(dir, "db"), base)
	mustLocal(t, r, base, blob)
	local := r.Brain(tenant.Local) // handle predates registration
	ma := mustProvision(t, r, a, tenantfs.Overrides{})
	mb := mustProvision(t, r, b, tenantfs.Overrides{BrainRoot: filepath.Join(dir, "other"), BlobRoot: filepath.Join(blob, "tenants", "b")})
	for _, root := range []string{base, blob, ma.BrainRoot, ma.BlobRoot, mb.BrainRoot, mb.BlobRoot} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(ma.BrainRoot, filepath.Join(base, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mb.BlobRoot, filepath.Join(base, "blob-alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(ma.BrainRoot, "back")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ma.BrainRoot, filepath.Join(base, "dangling-foreign")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tenants/tenant-a/note.md", "tenants/tenant-a/missing/deep.md", "alias/note.md", "alias/missing.md", "blob-alias/note.md", "alias/back/note.md", "tenants/unregistered/note.md", "../outside", "/absolute", "", ".", "x/../note.md", "nul\x00"} {
		t.Run(name, func(t *testing.T) {
			if _, err := local.ResolveForWrite(ctx, name); err == nil {
				t.Fatalf("write admitted foreign/invalid %q", name)
			}
			if _, err := local.Resolve(ctx, name); err == nil {
				t.Fatalf("read admitted foreign/invalid %q", name)
			}
		})
	}
	if _, err := r.Blobs(tenant.Local).ResolveForWrite(ctx, "tenants/b/new"); err == nil {
		t.Fatal("local blob admitted foreign blob")
	}
	if _, err := local.Resolve(ctx, "note.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.ResolveForWrite(ctx, "new/nested.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Resolve(ctx, "new/nested.md"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing read: %v", err)
	}
	if err := local.AdmitTraversal(ctx, "."); err != nil {
		t.Fatalf("cannot traverse local root: %v", err)
	}
	for _, name := range []string{"tenants", "alias", "blob-alias"} {
		if err := local.AdmitTraversal(ctx, name); err == nil {
			t.Fatalf("traversal admitted %s", name)
		}
	}
	for _, name := range []string{".", "tenants", "tenants/tenant-a", "alias", "blob-alias"} {
		if err := local.PreflightDelete(ctx, name); err == nil {
			t.Fatalf("delete admitted %s", name)
		}
	}
	if err := local.PreflightDelete(ctx, "new/missing"); err != nil {
		t.Fatalf("safe missing deletion: %v", err)
	}
	if err := local.PreflightDelete(ctx, "note.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Brain(a).Resolve(ctx, "note.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Brain(a).Resolve(ctx, "back/note.md"); err == nil {
		t.Fatal("foreign ancestor escape admitted")
	}
}

func TestProvisioningConflicts(t *testing.T) {
	for _, kind := range []string{"same", "nested", "ancestor", "blob-cross", "symlink", "missing-symlink", "outside-reserved"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "brain")
			_, r := open(t, filepath.Join(dir, "db"), base)
			mustLocal(t, r, base, filepath.Join(dir, "cas"))
			ma := mustProvision(t, r, a, tenantfs.Overrides{})
			root := ma.BrainRoot
			switch kind {
			case "nested":
				root = filepath.Join(root, "nested")
			case "ancestor":
				root = filepath.Dir(root)
			case "blob-cross":
				root = ma.BlobRoot
			case "outside-reserved":
				root = filepath.Join(base, "projects", "foreign")
			case "symlink", "missing-symlink":
				if err := os.MkdirAll(ma.BrainRoot, 0700); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(dir, "alias")
				if err := os.Symlink(ma.BrainRoot, alias); err != nil {
					t.Fatal(err)
				}
				root = alias
				if kind == "missing-symlink" {
					root = filepath.Join(alias, "missing")
				}
			}
			if _, err := r.Provision(ctx, b, tenantfs.Overrides{BrainRoot: root}); !errors.Is(err, tenantfs.ErrConflict) {
				t.Fatalf("overlap accepted: %v", err)
			}
			if _, err := r.Lookup(ctx, b); !errors.Is(err, tenantfs.ErrUnknown) {
				t.Fatalf("failed provisioning left row: %v", err)
			}
		})
	}
}

func TestInvalidUnknownAndClosedRepository(t *testing.T) {
	dir := t.TempDir()
	s, r := open(t, filepath.Join(dir, "db"), filepath.Join(dir, "brain"))
	for _, id := range []tenant.ID{{}, a} {
		if _, err := r.Lookup(ctx, id); err == nil {
			t.Fatal("invalid/unknown ID resolved")
		}
		if _, err := r.Brain(id).ResolveForWrite(ctx, "note"); err == nil {
			t.Fatal("invalid/unknown root admitted")
		}
	}
	for _, id := range []tenant.ID{{}, tenant.Local} {
		if _, err := r.Provision(ctx, id, tenantfs.Overrides{}); err == nil {
			t.Fatal("invalid provisioning")
		}
	}
	for _, raw := range []string{"../escape", "UPPER", "a/b", ""} {
		id, err := tenant.Parse(raw)
		if err == nil {
			t.Fatal("bad ID accepted")
		}
		if _, err = r.Lookup(ctx, id); err == nil {
			t.Fatal("failed parse broadened scope")
		}
	}
	mustLocal(t, r, filepath.Join(dir, "brain"), filepath.Join(dir, "cas"))
	for _, digest := range []string{"", strings.Repeat("A", 64), "../bad", strings.Repeat("a", 63)} {
		if _, err := r.BlobPath(ctx, tenant.Local, digest); err == nil {
			t.Fatal("invalid digest")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Brain(tenant.Local).ResolveForWrite(ctx, "note"); err == nil {
		t.Fatal("closed repository failed open")
	}
}

func TestConcurrentOverlappingProvision(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db")
	base := filepath.Join(dir, "brain")
	_, r1 := open(t, db, base)
	_, r2 := open(t, db, base)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, r := range []*tenantfs.Resolver{r1, r2} {
		wg.Add(1)
		go func(i int, r *tenantfs.Resolver) {
			defer wg.Done()
			id := a
			if i == 1 {
				id = b
			}
			_, err := r.Provision(ctx, id, tenantfs.Overrides{BrainRoot: filepath.Join(dir, "shared-root")})
			results <- err
		}(i, r)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("want exactly one successful registration, got %d", success)
	}
}

func TestSymlinkTargetCannotTransitForeignRoot(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "brain")
	_, r := open(t, filepath.Join(dir, "db"), base)
	mustLocal(t, r, base, filepath.Join(dir, "cas"))
	m := mustProvision(t, r, a, tenantfs.Overrides{})
	if err := os.MkdirAll(m.BrainRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "note.md"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(m.BrainRoot, "back")); err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks alone hides the intermediate foreign directory completely.
	if err := os.Symlink(filepath.Join(m.BrainRoot, "back", "note.md"), filepath.Join(base, "bounce")); err != nil {
		t.Fatal(err)
	}
	root := r.Brain(tenant.Local)
	if _, err := root.Resolve(ctx, "bounce"); !errors.Is(err, tenantfs.ErrDenied) {
		t.Fatalf("symlink target transited foreign root: %v", err)
	}
	if _, err := root.ResolveForWrite(ctx, "bounce"); !errors.Is(err, tenantfs.ErrDenied) {
		t.Fatalf("write transited foreign root: %v", err)
	}
	if err := os.Symlink(filepath.Join(base, "note.md"), filepath.Join(base, "safe-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Resolve(ctx, "safe-link"); err != nil {
		t.Fatalf("safe in-root alias: %v", err)
	}
}

func TestRootAliasesAndFilesystemFailures(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	_, r := open(t, filepath.Join(dir, "db"), alias)
	mustLocal(t, r, alias+"/./", filepath.Join(dir, "cas"))
	m := mustProvision(t, r, a, tenantfs.Overrides{})
	if _, err := r.Provision(ctx, b, tenantfs.Overrides{BrainRoot: filepath.Join(actual, "tenants", a.String())}); !errors.Is(err, tenantfs.ErrConflict) {
		t.Fatalf("canonical root alias overlap: %v", err)
	}
	root := r.Brain(tenant.Local)
	if err := os.WriteFile(filepath.Join(actual, "file"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(actual, "absent"), filepath.Join(actual, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(actual, "loop"), filepath.Join(actual, "loop")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file/child", "dangling/new", "loop/new"} {
		if _, err := root.ResolveForWrite(ctx, name); err == nil {
			t.Fatalf("unsafe missing path %s admitted", name)
		}
	}
	// Missing registered roots remain exclusions, including physical aliases.
	if _, err := root.ResolveForWrite(ctx, "tenants/"+a.String()+"/absent"); err == nil {
		t.Fatal("missing registered root admitted")
	}
	if err := root.PreflightDelete(ctx, "tenants"); err == nil {
		t.Fatal("missing foreign parent deletion admitted")
	}
	if err := os.MkdirAll(filepath.Dir(m.BrainAbsolute), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), m.BrainAbsolute); err != nil {
		t.Fatal(err)
	}
	if _, err := root.ResolveForWrite(ctx, "safe"); err == nil {
		t.Fatal("root drift failed open")
	}
}

func TestResolveRejectsAliasNamingRoot(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "brain")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	_, r := open(t, filepath.Join(dir, "db"), base)
	mustLocal(t, r, base, filepath.Join(dir, "cas"))
	if err := os.Symlink(base, filepath.Join(base, "self")); err != nil {
		t.Fatal(err)
	}
	root := r.Brain(tenant.Local)
	for _, resolve := range []func(context.Context, string) (string, error){root.Resolve, root.ResolveForWrite} {
		if _, err := resolve(ctx, "self"); !errors.Is(err, tenantfs.ErrDenied) {
			t.Errorf("root alias admitted as non-root target: %v", err)
		}
	}
	if err := root.AdmitTraversal(ctx, "."); err != nil {
		t.Fatalf("explicit traversal root denied: %v", err)
	}
}
