package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestBulkLegacyProjectScope(t *testing.T) {
	svc, _, dir := newTestBrainService(t)
	ctx := context.Background()
	// Hand-authored and old tasks need not carry projectId frontmatter.
	for _, project := range []string{"legacy_proj", "legacyXproj", "legacy_proj-other"} {
		for i := 0; i < 142; i++ {
			path := fmt.Sprintf("projects/%s/task/old-%03d.md", project, i)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
				t.Fatal(err)
			}
			content := "---\ntitle: Legacy\ntype: task\nstatus: completed\nfeature_id: old\n---\nbody\n"
			if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if err := svc.indexer.IndexFile(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	filter := &types.BulkUpdateFilter{Project: strPtr("legacy_proj"), Type: strPtr("task"), Status: strPtr("completed")}
	updated := 0
	for i := 0; i < 3; i++ {
		r, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{Filter: filter, Updates: &types.UpdateEntryRequest{Status: strPtr("archived")}})
		if err != nil {
			t.Fatal(err)
		}
		updated += r.Updated
		if !r.Truncated {
			break
		}
	}
	if updated != 142 {
		t.Fatalf("archived %d visible legacy tasks, want 142", updated)
	}
	filter.Status = strPtr("archived")
	deleted := 0
	for i := 0; i < 3; i++ {
		r, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		deleted += r.Deleted
		if !r.Truncated {
			break
		}
	}
	if deleted != 142 {
		t.Fatalf("deleted %d archived legacy tasks, want 142", deleted)
	}
	for _, project := range []string{"legacyXproj", "legacy_proj-other"} {
		files, _ := filepath.Glob(filepath.Join(dir, "projects", project, "task", "*.md"))
		if len(files) != 142 {
			t.Fatalf("sibling project %s was touched: %d files", project, len(files))
		}
	}
}

func TestLegacyRemoteRetirementOnly(t *testing.T) {
	for _, mode := range []string{"update", "metadata"} {
		for _, status := range []string{"archived", "cancelled", "superseded"} {
			t.Run(mode+"/"+status, func(t *testing.T) {
				svc, _, dir := newTestBrainService(t)
				ctx := context.Background()
				path := "projects/p/task/legacy.md"
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
					t.Fatal(err)
				}
				content := "---\ntitle: Legacy\ntype: task\nstatus: completed\nprojectId: p\ngit_remote: ssh://git@github.com/example/synthetic.git\n---\nbody\n"
				if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				if err := svc.indexer.IndexFile(path); err != nil {
					t.Fatal(err)
				}
				var err error
				if mode == "update" {
					_, err = svc.Update(ctx, path, types.UpdateEntryRequest{Status: &status})
				} else {
					_, err = svc.UpdateMetadata(ctx, path, map[string]interface{}{"status": status})
				}
				if err != nil {
					t.Fatalf("retiring a legacy task must not require runnable git credentials: %v", err)
				}
				e, err := svc.Recall(ctx, path)
				if err != nil || e.Status != status {
					t.Fatalf("status not persisted: %+v %v", e, err)
				}

				for _, reopen := range []string{"pending", "in_progress", "completed", "validated"} {
					if _, err = svc.Update(ctx, path, types.UpdateEntryRequest{Status: &reopen}); err == nil {
						t.Fatalf("unsupported remote transition to %s must still be refused", reopen)
					}
					if _, err = svc.UpdateMetadata(ctx, path, map[string]interface{}{"status": reopen}); err == nil {
						t.Fatalf("metadata transition to %s must still be refused", reopen)
					}
				}
				if _, err = svc.Update(ctx, path, types.UpdateEntryRequest{Status: strPtr("archived"), Executor: strPtr("pi")}); err == nil {
					t.Fatal("retirement must not bypass execution configuration admission")
				}

				if _, err = svc.UpdateMetadata(ctx, path, map[string]interface{}{"status": "archived", "git_remote": "ssh://git@github.com/other/repo"}); err == nil {
					t.Fatal("retirement must not bypass new remote admission")
				}
			})
		}
	}
}

func TestMoveSameProjectPreservesEntry(t *testing.T) {
	svc, _, dir := newTestBrainService(t)
	ctx := context.Background()
	e, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Same project", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, e.Path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Move(ctx, e.Path, "p"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, e.Path))
	if err != nil || string(after) != string(before) {
		t.Fatalf("same-project move changed source: %v", err)
	}
	if _, err = svc.Recall(ctx, e.Path); err != nil {
		t.Fatal(err)
	}
}

func TestMoveCollisionPreservesBothEntries(t *testing.T) {
	svc, _, dir := newTestBrainService(t)
	ctx := context.Background()
	e, err := svc.Save(ctx, types.CreateEntryRequest{Project: "p", Type: "task", Title: "Source", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "projects", "q", "task", filepath.Base(e.Path))
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatal(err)
	}
	const contents = "---\ntitle: Existing destination\ntype: task\n---\nDo not overwrite me.\n"
	if err := os.WriteFile(dest, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Move(ctx, e.Path, "q"); err == nil {
		t.Fatal("move overwrote destination instead of refusing")
	}
	after, err := os.ReadFile(dest)
	if err != nil || string(after) != contents {
		t.Fatal("destination changed")
	}
	if _, err = os.Stat(filepath.Join(dir, e.Path)); err != nil {
		t.Fatal("source lost")
	}
}
