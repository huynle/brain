package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/brainpath"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/internal/types"
)

// Unlike the unbound P1 containment and P2 Git fixtures, every operation here
// uses the same persisted local root policy and TenantStore. Independent denials
// prove neither guard masks a missing other guard; combined denials require no
// error precedence, but still prohibit writes, indexing and supersession.
func TestBoundTenantGitAdmissionBeforeMutation(t *testing.T) {
	for _, op := range []string{"save", "update", "metadata", "checkout"} {
		for _, mode := range []string{"allowed", "unsupported", "outside", "excluded", "outside+unsupported", "excluded+unsupported", "dangling+unsupported"} {
			if mode == "dangling+unsupported" && op != "checkout" {
				continue
			}
			t.Run(op+"/"+mode, func(t *testing.T) {
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
				bus := &recordingBus{}
				svc = NewBrainService(svc.config, store, idx, bus, nil)
				tasks := NewTaskService(svc.config, store, idx)
				insertRunnerForTaskSelectionTest(t, store, "configured", nil, []string{"git-credential-host:supported.invalid"})
				remote := "https://supported.invalid/o/r"
				saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Original", Status: "completed", FeatureID: "feature", GitRemote: remote})
				if err != nil {
					t.Fatal(err)
				}
				taskDir := filepath.Join(root, "projects/p/task")
				// An existing AI checkout makes premature supersession observable.
				checkoutPath := filepath.Join(taskDir, "checkout.md")
				checkout := "---\ntype: task\ntitle: Checkout\nstatus: pending\nfeature_id: feature\ngenerated: true\ngenerated_key: feature-checkout:feature:round-1\ncheckout_mode: ai\n---\n"
				if err := os.WriteFile(checkoutPath, []byte(checkout), 0600); err != nil {
					t.Fatal(err)
				}
				if err := idx.IndexFile("projects/p/task/checkout.md"); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(mode, "unsupported") {
					remote = "https://unsupported.invalid/o/r"
					if op == "checkout" {
						// Simulate legacy/out-of-band source metadata, not a Save
						// that would already reject the unsupported remote.
						file := filepath.Join(root, saved.Path)
						body, err := os.ReadFile(file)
						if err != nil {
							t.Fatal(err)
						}
						body = []byte(strings.ReplaceAll(string(body), "supported.invalid", "unsupported.invalid"))
						if err := os.WriteFile(file, body, 0600); err != nil {
							t.Fatal(err)
						}
						if err := idx.IndexFile(saved.Path); err != nil {
							t.Fatal(err)
						}
					}
				}
				outside := t.TempDir()
				if mode == "dangling+unsupported" {
					if err := os.Symlink(filepath.Join(root, "missing.md"), filepath.Join(taskDir, "dangling.md")); err != nil {
						t.Fatal(err)
					}
					guard := absoluteFilesystemGuard(ctx, idx, root)
					if _, err := tasks.getFeatureTasksFromFilesystem("p", "feature"); err == nil {
						t.Error("feature sources swallowed dangling child admission failure")
					}
					if _, err := findCheckoutTaskByKey(root, "projects/p/task", "feature-checkout:feature:round-1", guard); err == nil {
						t.Error("checkout lookup swallowed dangling child admission failure")
					}
					if _, err := findGeneratedTaskByKey(root, "projects/p/task", "feature-checkout:feature:round-1", guard); err == nil {
						t.Error("schedule lookup swallowed dangling child admission failure")
					}
				}
				if strings.HasPrefix(mode, "outside") || strings.HasPrefix(mode, "excluded") {
					target := filepath.Join(outside, "task")
					if strings.HasPrefix(mode, "excluded") {
						target = filepath.Join(root, "tenants/foreign/task")
					}
					if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(taskDir, target); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, taskDir); err != nil {
						t.Fatal(err)
					}
				}
				before, err := store.ListNotes(ctx, &storage.ListOptions{})
				if err != nil {
					t.Fatal(err)
				}
				files, external := containmentTree(t, root), containmentTree(t, outside)
				bus.mu.Lock()
				eventCount := len(bus.events)
				bus.mu.Unlock()
				switch op {
				case "save":
					_, err = svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "New", GitRemote: remote})
				case "update":
					_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{Title: strPtr("Changed"), GitRemote: &remote})
				case "metadata":
					_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"title": "Changed", "status": "pending", "git_remote": remote})
				case "checkout":
					_, err = tasks.CheckoutFeature(ctx, "p", "feature", &types.FeatureCheckoutOptions{CheckoutMode: "simple"})
				}
				if mode == "allowed" {
					if err != nil {
						t.Fatalf("bound local supported operation: %v", err)
					}
					if reflect.DeepEqual(files, containmentTree(t, root)) {
						t.Fatal("positive control did not persist its mutation")
					}
					after, err := store.ListNotes(ctx, &storage.ListOptions{})
					if err != nil || reflect.DeepEqual(before, after) {
						t.Fatalf("positive control did not index its mutation: %v", err)
					}
					return
				}
				if err == nil {
					t.Error("forbidden operation admitted")
				}
				if mode == "unsupported" && !errors.Is(err, api.ErrInvalidInput) {
					t.Errorf("want Git admission input error: %v", err)
				}
				// Bound service paths deny through tenantfs; checkout's directory
				// preflight may encounter the independent brainpath guard first.
				if mode == "outside" && !errors.Is(err, brainpath.ErrContainment) && !errors.Is(err, tenantfs.ErrDenied) {
					t.Errorf("want filesystem admission error: %v", err)
				}
				if mode == "excluded" && !errors.Is(err, tenantfs.ErrDenied) {
					t.Errorf("want tenant exclusion error: %v", err)
				}
				if !reflect.DeepEqual(files, containmentTree(t, root)) || !reflect.DeepEqual(external, containmentTree(t, outside)) {
					t.Error("denial changed filesystem, alias, or checkout status")
				}
				after, err := store.ListNotes(ctx, &storage.ListOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Error("denial changed indexed rows")
				}
				bus.mu.Lock()
				defer bus.mu.Unlock()
				if len(bus.events) != eventCount {
					t.Error("denial published a mutation event")
				}
			})
		}
	}
}
