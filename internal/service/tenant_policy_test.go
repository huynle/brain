package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/internal/types"
)

func TestTaskLookupsPropagatePolicyFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "task0001.md"), []byte("task"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("registry unavailable")
	guard := func(path string) error {
		if path != dir {
			return failure
		}
		return nil
	}
	if _, err := findCheckoutTaskByKey(filepath.Dir(dir), filepath.Base(dir), "key", guard); !errors.Is(err, failure) {
		t.Errorf("checkout swallowed policy failure: %v", err)
	}
	if _, err := findGeneratedTaskByKey(filepath.Dir(dir), filepath.Base(dir), "key", guard); !errors.Is(err, failure) {
		t.Errorf("schedule swallowed policy failure: %v", err)
	}
}

func TestTaskLookupsMissingDirectoryOnly(t *testing.T) {
	tasks, _, root := newTestTaskService(t)
	name := "projects/absent/task"
	if got, err := tasks.getFeatureTasksFromFilesystem("absent", "f"); err != nil || len(got) != 0 {
		t.Fatalf("missing task directory: %v %v", got, err)
	}
	if got, err := findCheckoutTaskByKey(root, name, "key"); err != nil || got != nil {
		t.Fatalf("missing checkout directory: %v %v", got, err)
	}
	if got, err := findGeneratedTaskByKey(root, name, "key"); err != nil || got != nil {
		t.Fatalf("missing schedule directory: %v %v", got, err)
	}
	// Even if the directory is absent, a policy ENOENT is not an empty listing.
	guard := func(string) error { return os.ErrNotExist }
	if _, err := findCheckoutTaskByKey(root, name, "key", guard); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("checkout swallowed directory policy failure: %v", err)
	}
	if _, err := findGeneratedTaskByKey(root, name, "key", guard); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("schedule swallowed directory policy failure: %v", err)
	}
}

func TestInjectGateDependencyChildENOENTFailsClosed(t *testing.T) {
	ctx := context.Background()
	svc, store, root := newTestBrainService(t)
	roots, err := tenantfs.New(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(ctx, root, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := indexer.NewIndexer(root, store, roots.Brain(tenant.Local))
	svc = NewBrainService(svc.config, store, idx, nil, nil)
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Keep", FeatureID: "f"})
	if err != nil {
		t.Fatal(err)
	}
	// Directory admission and the real task's admission succeed before the
	// lexically last child's dangling target fails tenant policy resolution.
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "projects/p/task/zzzzzzzz.md")); err != nil {
		t.Fatal(err)
	}
	files := containmentTree(t, root)
	before, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.injectGateDependency(ctx, "p", "f", "gate0001"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("late child policy ENOENT became false success: %v", err)
	}
	after, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, containmentTree(t, root)) || !reflect.DeepEqual(before, after) {
		t.Error("failed preflight mutated file or indexed task")
	}
	if err := svc.injectGateDependency(ctx, "absent", "f", "gate0001"); err != nil {
		t.Fatalf("genuinely absent task directory: %v", err)
	}
}

func TestTaskDirectoryReadErrorsFailClosed(t *testing.T) {
	tasks, store, root := newTestTaskService(t)
	brain := NewBrainService(tasks.config, store, tasks.indexer, nil, nil)
	name := "projects/p/task"
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "unread01.md")
	if err := os.WriteFile(file, []byte("---\ntype: task\nfeature_id: f\n---\n"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0600) })
	if _, err := os.ReadFile(file); err == nil {
		t.Skip("requires unreadable file permissions")
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"checkout lookup", func() error { _, err := findCheckoutTaskByKey(root, name, "key"); return err }},
		{"schedule lookup", func() error { _, err := findGeneratedTaskByKey(root, name, "key"); return err }},
		{"feature sources", func() error { _, err := tasks.getFeatureTasksFromFilesystem("p", "f"); return err }},
		{"gate injection", func() error { return brain.injectGateDependency(context.Background(), "p", "f", "gate0001") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("read failure became successful/empty result: %v", err)
			}
		})
	}
}

type transientRootsRepository struct {
	tenantfs.Repository
	calls, failAt int
}

var errTransientRoots = errors.New("transient registry failure")

func (r *transientRootsRepository) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	r.calls++
	if r.calls == r.failAt {
		return nil, errTransientRoots
	}
	return r.Repository.ListTenantRoots(ctx)
}

func TestServicePropagatesTransientPolicyFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := &transientRootsRepository{Repository: store}
	r, err := tenantfs.New(repo, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(ctx, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := indexer.NewIndexer(dir, store, r.Brain(tenant.Local))
	cfg := &config.Config{BrainDir: dir}
	svc := NewBrainService(cfg, store, idx, nil, nil)
	tasks := NewTaskService(cfg, store, idx)
	if _, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Task", FeatureID: "f"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		failAt int
		run    func() error
	}{
		{"list", 1, func() error { _, err := svc.List(ctx, types.ListEntriesRequest{}); return err }},
		{"projects", 2, func() error { _, err := tasks.ListProjects(ctx); return err }},
		{"task read", 2, func() error { _, err := tasks.getFeatureTasksFromFilesystem("p", "f"); return err }},
		{"dependency injection", 2, func() error { return svc.injectGateDependency(ctx, "p", "f", "gate0001") }},
		{"dependency update admission", 4, func() error { return svc.injectGateDependency(ctx, "p", "f", "gate0001") }},
		{"schedule lookup", 2, func() error {
			return svc.ensureFeatureScheduleGate(ctx, "p", "f", FeatureScheduleFields{Schedule: "0 1 * * *"})
		}},
		{"checkout lookup", 2, func() error { _, err := tasks.CheckoutFeature(ctx, "p", "f", nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo.calls, repo.failAt = 0, tc.failAt
			if err := tc.run(); !errors.Is(err, errTransientRoots) {
				t.Errorf("policy failure swallowed: %v", err)
			}
			repo.failAt = 0
		})
	}
}

func TestDurableMetadataPolicyFailureDoesNotFallBackToDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := &transientRootsRepository{Repository: store}
	roots, err := tenantfs.New(repo, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(ctx, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := indexer.NewIndexer(dir, store, roots.Brain(tenant.Local))
	svc := NewBrainService(&config.Config{BrainDir: dir}, store, idx, nil, nil)
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Keep", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, saved.Path)
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// resolveEntry admits the row first. Fail the second admission, inside
	// durable sync, where the legacy best-effort fallback used to hide errors.
	repo.calls, repo.failAt = 0, 2
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "completed"})
	if !errors.Is(err, errTransientRoots) {
		t.Errorf("durable sync swallowed registry failure: %v", err)
	}
	repo.failAt = 0
	after, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Error("database changed after durable admission failure")
	}
	afterBody, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBody) != string(body) {
		t.Error("file changed after durable admission failure")
	}
}

func TestDurableMetadataReindexFailureDoesNotFallBackToDatabase(t *testing.T) {
	ctx := context.Background()
	svc, store, dir := newTestBrainService(t)
	repo := &transientRootsRepository{Repository: store}
	roots, err := tenantfs.New(repo, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(ctx, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := indexer.NewIndexer(dir, store, roots.Brain(tenant.Local))
	bus := &recordingBus{}
	svc = NewBrainService(svc.config, store, idx, bus, nil)
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Keep", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, saved.Path)
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	bus.mu.Lock()
	eventCount := len(bus.events)
	bus.mu.Unlock()
	// Row admission and durable file admission succeed; IndexFile's third
	// registry lookup fails AFTER the file write, before its DB update.
	repo.calls, repo.failAt = 0, 3
	result, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"title": "Written before reindex", "resume_requested": true})
	if !errors.Is(err, errTransientRoots) || result != nil {
		t.Errorf("reindex failure returned success: result=%v err=%v", result, err)
	}
	if repo.calls != 3 {
		t.Errorf("expected failure at reindex lookup 3, got %d", repo.calls)
	}
	repo.failAt = 0
	after, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Error("reindex failure fell back to DB mutation")
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.events) != eventCount {
		t.Error("reindex failure emitted success event")
	}
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) == string(body) {
		t.Fatal("fixture did not reach post-write reindex failure")
	}
}

func TestTenantPolicyService(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := tenantfs.New(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(ctx, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := indexer.NewIndexer(dir, store, r.Brain(tenant.Local))
	cfg := &config.Config{BrainDir: dir}
	svc := NewBrainService(cfg, store, idx, nil, nil)
	tasks := NewTaskService(cfg, store, idx)
	foreign := "tenants/foreign/projects/p/note/foreign1.md"
	abs := filepath.Join(dir, foreign)
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	// Simulate a stale pre-upgrade index without using the now-protected indexer.
	if _, err := store.InsertNote(ctx, &storage.NoteRow{Path: foreign, ShortID: "foreign1", Title: "Private", Metadata: "{}"}); err != nil {
		t.Fatal(err)
	}
	t.Run("foreign read", func(t *testing.T) {
		if _, err := svc.Recall(ctx, foreign); err == nil {
			t.Error("read admitted foreign path")
		}
	})
	t.Run("foreign listing", func(t *testing.T) {
		listed, err := svc.List(ctx, types.ListEntriesRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range listed.Entries {
			if entry.Path == foreign {
				t.Error("listing exposed excluded indexed path")
			}
		}
	})
	t.Run("recursive preflight before mutation", func(t *testing.T) {
		saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "preflight", Type: "note", Title: "Retain", Content: "keep"})
		if err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(dir, "projects/preflight/alias.md")
		if err := os.Symlink(abs, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DeleteProject(ctx, "preflight"); err == nil {
			t.Error("purge admitted tree containing excluded alias")
		}
		if _, err := os.Stat(filepath.Join(dir, saved.Path)); err != nil {
			t.Errorf("preflight deleted owned file before rejecting tree: %v", err)
		}
	})
	t.Run("foreign delete", func(t *testing.T) {
		if err := svc.Delete(ctx, foreign); err == nil {
			t.Error("delete admitted foreign path")
		}
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("foreign file changed: %v", err)
		}
	})
	t.Run("project alias", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(dir, "projects"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "tenants/foreign/projects/p"), filepath.Join(dir, "projects/alias")); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DeleteProject(ctx, "alias"); err == nil {
			t.Error("recursive delete admitted foreign alias")
		}
	})
	t.Run("local", func(t *testing.T) {
		saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "local", Type: "task", Title: "Local", Content: "body"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Recall(ctx, saved.Path); err != nil {
			t.Fatal(err)
		}
		body := "updated body"
		if _, err := svc.Update(ctx, saved.Path, types.UpdateEntryRequest{Content: &body}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.UpdateMetadata(ctx, saved.Path, map[string]interface{}{"priority": "high"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Move(ctx, saved.Path, "moved"); err != nil {
			t.Fatal(err)
		}
		entry, err := svc.Recall(ctx, saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		saved.Path = entry.Path
		if _, err := tasks.ListProjects(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, saved.Path); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DeleteProject(ctx, "moved"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("foreign write and move", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(dir, "projects/move-target"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "tenants"), filepath.Join(dir, "projects/move-target/note")); err != nil {
			t.Fatal(err)
		}
		saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "source", Type: "note", Title: "Source", Content: "keep"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Move(ctx, saved.Path, "move-target"); err == nil {
			t.Error("move admitted foreign destination")
		}
		if _, err := os.Stat(filepath.Join(dir, saved.Path)); err != nil {
			t.Errorf("move removed source: %v", err)
		}
		if _, err := svc.Save(ctx, types.CreateEntryRequest{Project: "move-target", Type: "note", Title: "Denied"}); err == nil {
			t.Error("save admitted foreign destination")
		}
	})
	t.Run("child aliases in task helpers", func(t *testing.T) {
		dirName := filepath.Join(dir, "projects/children/task")
		if err := os.MkdirAll(dirName, 0700); err != nil {
			t.Fatal(err)
		}
		foreignFile := filepath.Join(dir, "tenants/child.md")
		content := []byte("---\ntitle: Foreign\ntype: task\nfeature_id: f\ngenerated_key: feature-checkout:f:round-1\nstatus: pending\n---\nbody")
		if err := os.WriteFile(foreignFile, content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreignFile, filepath.Join(dirName, "child001.md")); err != nil {
			t.Fatal(err)
		}
		entries, err := tasks.getFeatureTasksFromFilesystem("children", "f")
		if !errors.Is(err, tenantfs.ErrDenied) {
			t.Fatalf("excluded child must fail preflight: %v", err)
		}
		if len(entries) != 0 {
			t.Error("task reader followed excluded child")
		}
		guard := absoluteFilesystemGuard(ctx, idx, dir)
		if entry, _ := findCheckoutTaskByKey(dir, "projects/children/task", "feature-checkout:f:round-1", guard); entry != nil {
			t.Error("checkout lookup followed excluded child")
		}
		if entry, _ := findGeneratedTaskByKey(dir, "projects/children/task", "feature-checkout:f:round-1", guard); entry != nil {
			t.Error("schedule lookup followed excluded child")
		}
	})
	t.Run("task and feature paths", func(t *testing.T) {
		foreignTaskDir := filepath.Join(dir, "tenants/scheduled/task")
		if err := os.MkdirAll(foreignTaskDir, 0700); err != nil {
			t.Fatal(err)
		}
		content := []byte("---\ntitle: Foreign task\ntype: task\nfeature_id: f\ngenerated_key: feature-schedule:f\n---\nprivate")
		if err := os.WriteFile(filepath.Join(foreignTaskDir, "foreign2.md"), content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "projects/scheduled"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreignTaskDir, filepath.Join(dir, "projects/scheduled/task")); err != nil {
			t.Fatal(err)
		}
		projects, err := tasks.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range projects {
			if p == "scheduled" {
				t.Error("listing admitted foreign task directory")
			}
		}
		if _, err := tasks.CheckoutFeature(ctx, "scheduled", "f", nil); err == nil {
			t.Error("checkout wrote into foreign directory")
		}
		entries, err := os.ReadDir(foreignTaskDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Error("checkout mutated foreign directory before returning error")
		}
		if _, err := tasks.getFeatureTasksFromFilesystem("scheduled", "f"); err == nil {
			t.Error("feature task read admitted foreign directory")
		}
		if err := svc.injectGateDependency(ctx, "scheduled", "f", "gate0001"); err == nil {
			t.Error("dependency injection read foreign directory")
		}
		if err := svc.ensureFeatureScheduleGate(ctx, "scheduled", "f", FeatureScheduleFields{Schedule: "0 1 * * *"}); err == nil {
			t.Error("schedule admitted foreign directory")
		}
	})
}

func TestUnboundFilesystemPathCompatibility(t *testing.T) {
	for _, name := range []string{"../escape", ".", "/tmp/escape"} {
		if _, err := resolveFilesystemPath(context.Background(), nil, t.TempDir(), name, true); err == nil {
			t.Errorf("unbound path accepted %q", name)
		}
	}
}
