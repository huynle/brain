package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/brainpath"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func containmentFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ntitle: Original\ntype: note\nstatus: active\n---\nSentinel\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func containmentLink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// Snapshot links themselves, not their targets, so rejected operations must
// preserve lexical files/directories as well as the external sentinel.
func containmentTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			out[path] = "link:" + target
			return err
		}
		if d.IsDir() {
			out[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		out[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContainmentStoredPaths(t *testing.T) {
	for _, kind := range []string{"parent", "symlink", "dangling"} {
		for _, op := range []string{"update", "delete", "move", "metadata"} {
			t.Run(kind+"/"+op, func(t *testing.T) {
				svc, store, sandbox := nestedValidationBrain(t)
				root := svc.config.BrainDir
				outside := filepath.Join(filepath.Dir(root), "escape", "x.md")
				containmentFile(t, outside)
				path := "../escape/x.md"
				if kind != "parent" {
					path = "projects/source/note/x.md"
					target := outside
					if kind == "dangling" {
						target += ".missing"
					}
					containmentLink(t, target, filepath.Join(root, path))
				}
				ctx := context.Background()
				row, err := store.InsertNote(ctx, &storage.NoteRow{Path: path, ShortID: "poison01", Title: "Original", Type: strPtr("note"), Status: strPtr("active"), ProjectID: strPtr("source")})
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(outside)
				if err != nil {
					t.Fatal(err)
				}
				tree := containmentTree(t, sandbox)
				switch op {
				case "update":
					_, err = svc.Update(ctx, "poison01", types.UpdateEntryRequest{Title: strPtr("Changed")})
				case "delete":
					err = svc.Delete(ctx, "poison01")
				case "move":
					_, err = svc.Move(ctx, "poison01", "destination")
				case "metadata":
					_, err = svc.UpdateMetadata(ctx, "poison01", map[string]interface{}{"title": "Changed"})
				}
				if !errors.Is(err, brainpath.ErrContainment) {
					t.Errorf("want ErrContainment, got %v", err)
				}
				if !reflect.DeepEqual(tree, containmentTree(t, sandbox)) {
					t.Error("filesystem mutated")
				}
				after, readErr := os.ReadFile(outside)
				if readErr != nil || string(before) != string(after) {
					t.Errorf("external sentinel changed: %v", readErr)
				}
				got, err := store.GetNoteByShortID(ctx, "poison01")
				if err != nil || !reflect.DeepEqual(row, got) {
					t.Errorf("index mutated: %v", err)
				}
				requireNoteCount(t, store, 1)
				if _, err := os.Stat(filepath.Join(root, "projects", "destination")); !os.IsNotExist(err) {
					t.Errorf("destination mutated: %v", err)
				}
			})
		}
	}
}

func TestContainmentDestinations(t *testing.T) {
	for _, op := range []string{"save", "move", "stats"} {
		t.Run(op, func(t *testing.T) {
			svc, store, sandbox := nestedValidationBrain(t)
			ctx := context.Background()
			saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "source", Type: "note", Title: "Original", Content: "unchanged"})
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(sandbox, "outside")
			containmentFile(t, filepath.Join(outside, "sentinel.md"))
			link := filepath.Join(svc.config.BrainDir, "projects", "destination")
			if op == "stats" {
				link = filepath.Join(svc.config.BrainDir, ".brain.db")
			}
			containmentLink(t, outside, link)
			before := containmentTree(t, sandbox)
			row, _ := store.GetNoteByShortID(ctx, saved.ID)
			switch op {
			case "save":
				_, err = svc.Save(ctx, types.CreateEntryRequest{Project: "destination", Type: "note", Title: "New", Content: "body"})
			case "move":
				_, err = svc.Move(ctx, saved.ID, "destination")
			case "stats":
				_, err = svc.GetStats(ctx, false, "", nil)
			}
			if !errors.Is(err, brainpath.ErrContainment) {
				t.Errorf("want ErrContainment, got %v", err)
			}
			if !reflect.DeepEqual(before, containmentTree(t, sandbox)) {
				t.Error("filesystem mutated")
			}
			after, _ := store.GetNoteByShortID(ctx, saved.ID)
			if !reflect.DeepEqual(row, after) {
				t.Error("source index mutated")
			}
			requireNoteCount(t, store, 1)
		})
	}
}

func TestContainmentDirectoryScans(t *testing.T) {
	for _, kind := range []string{"directory", "child"} {
		for _, op := range []string{"projects", "checkout", "enumerate", "ensure", "inject", "project-delete"} {
			t.Run(kind+"/"+op, func(t *testing.T) {
				svc, store, sandbox := nestedValidationBrain(t)
				tasks := NewTaskService(svc.config, store, svc.indexer)
				outside := filepath.Join(sandbox, "outside")
				containmentFile(t, filepath.Join(outside, "x.md"))
				taskDir := filepath.Join(svc.config.BrainDir, "projects", "safe", "task")
				if kind == "directory" {
					containmentLink(t, outside, taskDir)
				} else {
					containmentLink(t, filepath.Join(outside, "x.md"), filepath.Join(taskDir, "x.md"))
				}
				before := containmentTree(t, sandbox)
				ctx := context.Background()
				var err error
				switch op {
				case "projects":
					if kind == "child" {
						t.Skip("ListProjects does not read task files")
					}
					_, err = tasks.ListProjects(ctx)
				case "checkout":
					_, err = tasks.CheckoutFeature(ctx, "safe", "feature", nil)
				case "enumerate":
					_, err = tasks.getFeatureTasksFromFilesystem("safe", "feature")
				case "ensure":
					err = svc.ensureFeatureScheduleGate(ctx, "safe", "feature", FeatureScheduleFields{Schedule: "0 3 * * *"})
				case "inject":
					err = svc.injectGateDependency(ctx, "safe", "feature", "gate1234")
				case "project-delete":
					_, err = svc.DeleteProject(ctx, "safe")
				}
				if !errors.Is(err, brainpath.ErrContainment) {
					t.Errorf("want ErrContainment, got %v", err)
				}
				if !reflect.DeepEqual(before, containmentTree(t, sandbox)) {
					t.Error("filesystem mutated")
				}
				requireNoteCount(t, store, 0)
				if _, err := os.Lstat(taskDir); err != nil {
					t.Errorf("project directory mutated: %v", err)
				}
			})
		}
	}
}

func TestContainmentDeleteCompatibility(t *testing.T) {
	for _, kind := range []string{"missing", "in-root-link"} {
		t.Run(kind, func(t *testing.T) {
			svc, store, root := newTestBrainService(t) // Includes macOS /var temp-root alias.
			ctx := context.Background()
			saved, err := svc.Save(ctx, types.CreateEntryRequest{Project: "safe", Type: "note", Title: "Original", Content: "body"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, saved.Path)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "target.md")
			if kind == "in-root-link" {
				containmentFile(t, target)
				containmentLink(t, target, path)
			}
			if err := svc.Delete(ctx, saved.ID); err != nil {
				t.Fatal(err)
			}
			requireNoteCount(t, store, 0)
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Errorf("lexical path remains: %v", err)
			}
			if kind == "in-root-link" {
				if _, err := os.Stat(target); err != nil {
					t.Errorf("target removed: %v", err)
				}
			}
		})
	}
}
