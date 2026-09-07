package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

var unsafePathSegments = []string{"..", "a/../../..", "/abs/path", ".", "", "bad\x00id", `a\b`, "a/b", "a..b", "../../../tmp/pwned"}

func TestPathValidation_ProjectPolicy(t *testing.T) {
	for _, id := range unsafePathSegments {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			if err := validateProjectID(id); err == nil {
				t.Errorf("expected validation error for %q", id)
			}
		})
	}
}

// Nest the brain sufficiently deeply that even the regression's traversal
// destinations remain inside this test's owned sandbox, including during RED.
func nestedValidationBrain(t *testing.T) (*BrainServiceImpl, *storage.TenantStore, string) {
	t.Helper()
	svc, store, sandbox := newTestBrainService(t)
	svc.config.BrainDir = filepath.Join(sandbox, "one", "two", "brain")
	if err := os.MkdirAll(svc.config.BrainDir, 0755); err != nil {
		t.Fatal(err)
	}
	svc.indexer = indexer.NewIndexer(svc.config.BrainDir, store)
	return svc, store, sandbox
}

func validationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		tree[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func requireSegmentError(t *testing.T, err error) {
	t.Helper()
	var pathErr *os.PathError
	if err == nil || errors.As(err, &pathErr) || (!strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "required")) {
		t.Errorf("expected segment validation error, got %v", err)
	}
}

func TestPathValidation_DeleteProjectNUL(t *testing.T) {
	svc, store, sandbox := nestedValidationBrain(t)
	before := validationTree(t, sandbox)
	_, err := svc.DeleteProject(context.Background(), "bad\x00id")
	requireSegmentError(t, err)
	if !reflect.DeepEqual(before, validationTree(t, sandbox)) {
		t.Error("invalid project changed filesystem")
	}
	requireNoteCount(t, store, 0)
}

func requireNoteCount(t *testing.T, store *storage.TenantStore, want int) {
	t.Helper()
	var got int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM notes").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("notes rows = %d, want %d", got, want)
	}
}

func TestPathValidation_Save(t *testing.T) {
	for _, field := range []string{"project", "type"} {
		for _, global := range []bool{false, true} {
			for _, segment := range unsafePathSegments {
				if field == "project" && segment == "" {
					continue // Save's documented default, tested below.
				}
				t.Run(fmt.Sprintf("%s/global=%t/%q", field, global, segment), func(t *testing.T) {
					svc, store, sandbox := nestedValidationBrain(t)
					before := validationTree(t, sandbox)
					req := types.CreateEntryRequest{Project: "safe", Type: "note", Title: "Rejected", Content: "body", Global: &global}
					if field == "project" {
						req.Project = segment
					} else {
						req.Type = segment
					}
					_, err := svc.Save(context.Background(), req)
					requireSegmentError(t, err)
					if !reflect.DeepEqual(before, validationTree(t, sandbox)) {
						t.Error("rejected Save changed filesystem (including escaped paths)")
					}
					requireNoteCount(t, store, 0)
				})
			}
		}
	}
}

func TestPathValidation_SaveCompatibility(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			svc, store, _ := nestedValidationBrain(t)
			resp, err := svc.Save(context.Background(), types.CreateEntryRequest{Type: "custom-type.v2", Title: "Custom", Content: "body", Global: &global})
			if err != nil {
				t.Fatal(err)
			}
			prefix := "projects/default/custom-type.v2/"
			if global {
				prefix = "global/custom-type.v2/"
			}
			if !strings.HasPrefix(resp.Path, prefix) {
				t.Errorf("path = %q, want prefix %q", resp.Path, prefix)
			}
			requireNoteCount(t, store, 1)
		})
	}
}

func TestPathValidation_Move(t *testing.T) {
	for _, target := range unsafePathSegments {
		t.Run(fmt.Sprintf("%q", target), func(t *testing.T) {
			svc, store, sandbox := nestedValidationBrain(t)
			ctx := context.Background()
			source, err := svc.Save(ctx, types.CreateEntryRequest{Project: "source", Type: "note", Title: "Original", Content: "unchanged"})
			if err != nil {
				t.Fatal(err)
			}
			before := validationTree(t, sandbox)
			row, err := store.GetNoteByShortID(ctx, source.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.Move(ctx, source.ID, target)
			requireSegmentError(t, err)
			if !reflect.DeepEqual(before, validationTree(t, sandbox)) {
				t.Error("rejected Move changed filesystem/source file")
			}
			after, err := store.GetNoteByShortID(ctx, source.ID)
			if err != nil || !reflect.DeepEqual(row, after) {
				t.Errorf("rejected Move changed source index: %v", err)
			}
			requireNoteCount(t, store, 1)
		})
	}
}

func TestPathValidation_TaskHelpers(t *testing.T) {
	for _, operation := range []string{"checkout", "enumerate", "resume"} {
		t.Run(operation, func(t *testing.T) {
			svc, store, sandbox := newTestTaskService(t)
			svc.config.BrainDir = filepath.Join(sandbox, "one", "two", "brain")
			if err := os.MkdirAll(svc.config.BrainDir, 0755); err != nil {
				t.Fatal(err)
			}
			svc.indexer = indexer.NewIndexer(svc.config.BrainDir, store)
			before := validationTree(t, sandbox)
			for _, project := range unsafePathSegments {
				var err error
				switch operation {
				case "checkout":
					_, err = svc.CheckoutFeature(context.Background(), " "+project+" ", "feature", nil)
				case "enumerate":
					_, err = svc.getFeatureTasksFromFilesystem(project, "feature")
				case "resume":
					_, err = svc.ResumeFeature(context.Background(), project, "feature", nil)
				}
				requireSegmentError(t, err)
			}
			if !reflect.DeepEqual(before, validationTree(t, sandbox)) {
				t.Error("invalid project changed filesystem")
			}
			requireNoteCount(t, store, 0)
		})
	}
}

func TestPathValidation_ScheduleHelpersBeforeExistingTaskMutation(t *testing.T) {
	for _, operation := range []string{"ensure", "inject"} {
		t.Run(operation, func(t *testing.T) {
			svc, store, sandbox := nestedValidationBrain(t)
			ctx := context.Background()
			req := types.CreateEntryRequest{Project: "victim", Type: "task", Title: "Existing", Content: "unchanged", FeatureID: "feature"}
			if operation == "ensure" {
				req.Generated = boolPtr(true)
				req.GeneratedKey = featureScheduleGeneratedKey("feature")
			}
			saved, err := svc.Save(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			before := validationTree(t, sandbox)
			row, err := store.GetNoteByShortID(ctx, saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			// This alias reaches the real indexed task without poisoning its row.
			project := "../projects/victim"
			if operation == "ensure" {
				err = svc.ensureFeatureScheduleGate(ctx, project, "feature", FeatureScheduleFields{Schedule: "0 3 * * *"})
			} else {
				err = svc.injectGateDependency(ctx, project, "feature", "gate1234")
			}
			requireSegmentError(t, err)
			if !reflect.DeepEqual(before, validationTree(t, sandbox)) {
				t.Error("invalid project mutated existing task file")
			}
			after, err := store.GetNoteByShortID(ctx, saved.ID)
			if err != nil || !reflect.DeepEqual(row, after) {
				t.Errorf("invalid project mutated existing task index: %v", err)
			}
			requireNoteCount(t, store, 1)
		})
	}
}
