package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

func TestGitRemoteSaveAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, remote     string
		advertise, allow bool
	}{
		{"no advertisements", "https://unsupported.invalid/org/repo", false, false},
		{"supported offline", "https://SUPPORTED.invalid:443/org/repo", true, true},
		{"different host", "https://unsupported.invalid/org/repo", true, false},
		{"different port", "https://supported.invalid:8443/org/repo", true, false},
		{"SSH", "git@supported.invalid:org/repo", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, dir := newTestBrainService(t)
			if tc.advertise {
				insertRunnerForTaskSelectionTest(t, store, "configured", nil, []string{"git-credential-host:supported.invalid"})
				r, _ := store.GetRunner(context.Background(), "configured")
				r.LastHeartbeat = time.Now().Add(-time.Hour).UnixMilli()
				if err := store.UpsertRunner(context.Background(), r); err != nil {
					t.Fatal(err)
				}
			}
			_, err := svc.Save(context.Background(), types.CreateEntryRequest{Type: "task", Title: "remote task", Project: "p", GitRemote: tc.remote, TargetWorkdir: "/local/repo", ExecutionMode: "current_branch"})
			if tc.allow {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsupported git_remote accepted before persistence")
			}
			if !errors.Is(err, api.ErrInvalidInput) {
				t.Fatalf("admission must classify client input: %v", err)
			}
			if strings.Contains(tc.remote, "unsupported.invalid") && !strings.Contains(err.Error(), "unsupported.invalid") {
				t.Fatalf("error does not name host: %v", err)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "projects", "p", "task", "*.md"))
			if len(files) != 0 {
				t.Fatalf("rejected task persisted: %v", files)
			}
			entries, err := svc.List(context.Background(), types.ListEntriesRequest{Project: "p", Type: "task"})
			if err != nil || len(entries.Entries) != 0 {
				t.Fatalf("rejected task indexed: %+v, %v", entries, err)
			}
		})
	}
}

func TestGitRemoteUpdatesRejectBeforeMutation(t *testing.T) {
	for _, mode := range []string{"update", "metadata", "metadata with durable", "metadata wrong type", "metadata type spoof", "metadata effective prior"} {
		t.Run(mode, func(t *testing.T) {
			svc, store, dir := newTestBrainService(t)
			ctx := context.Background()
			saved, err := svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "original", Project: "p"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "metadata effective prior" {
				if _, err := store.MergeMetadata(ctx, saved.Path, map[string]interface{}{"git_remote": "https://unsupported.invalid/o/r"}); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(filepath.Join(dir, saved.Path))
			rowBefore, _ := store.GetNoteByPath(ctx, saved.Path)
			remote := "https://unsupported.invalid/o/r"
			if mode == "update" {
				_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{GitRemote: &remote, Title: strPtr("changed")})
			} else {
				fields := map[string]interface{}{"git_remote": remote}
				switch mode {
				case "metadata with durable":
					fields["title"] = "changed"
				case "metadata wrong type":
					fields["git_remote"] = []string{remote}
				case "metadata type spoof":
					fields["type"] = "summary"
				case "metadata effective prior":
					fields = map[string]interface{}{"status": "pending"}
				}
				_, err = svc.UpdateMetadata(ctx, saved.ID, fields)
			}
			if err == nil {
				t.Fatal("unsupported effective git_remote update accepted")
			}
			if mode != "metadata wrong type" && !errors.Is(err, api.ErrInvalidInput) {
				t.Fatalf("admission must classify client input: %v", err)
			}
			after, _ := os.ReadFile(filepath.Join(dir, saved.Path))
			rowAfter, _ := store.GetNoteByPath(ctx, saved.Path)
			if string(before) != string(after) || rowBefore.Metadata != rowAfter.Metadata || rowBefore.Title != rowAfter.Title {
				t.Fatal("rejected update mutated file/index")
			}
		})
	}
}

func TestGitRemoteCheckoutRejectsBeforeSuperseding(t *testing.T) {
	svc, _, dir := newTestTaskService(t)
	taskDir := filepath.Join(dir, "projects", "p", "task")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	feature := "---\ntype: task\ntitle: Feature task\nstatus: completed\nfeature_id: feature\ngit_remote: https://unsupported.invalid/o/r\n---\n"
	existing := "---\ntype: task\ntitle: Checkout\nstatus: pending\nfeature_id: feature\ngenerated: true\ngenerated_key: feature-checkout:feature:round-1\ncheckout_mode: ai\n---\n"
	if err := os.WriteFile(filepath.Join(taskDir, "feature1.md"), []byte(feature), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "checkout.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CheckoutFeature(context.Background(), "p", "feature", &types.FeatureCheckoutOptions{CheckoutMode: "simple"})
	if err == nil || !strings.Contains(err.Error(), "unsupported.invalid") {
		t.Fatalf("checkout must refuse unsupported host: %v", err)
	}
	if !errors.Is(err, api.ErrInvalidInput) {
		t.Fatalf("checkout admission must classify client input: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(taskDir, "checkout.md"))
	if err != nil || string(after) != existing {
		t.Fatalf("rejected checkout superseded existing task: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(taskDir, "*.md"))
	if len(files) != 2 {
		t.Fatalf("rejected checkout wrote files: %v", files)
	}
}

func TestGitRemoteRegistryFailureRemainsInternal(t *testing.T) {
	_, store, _ := newTestBrainService(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	err := validateConfiguredGitRemote(context.Background(), store, "https://supported.invalid/o/r")
	var admissionErr gitRemoteAdmissionError
	if err == nil || errors.Is(err, api.ErrInvalidInput) || !errors.As(err, &admissionErr) {
		t.Fatalf("registry failure must remain internal but fail closed for metadata: %v", err)
	}
}

func TestGitRemoteMetadataCannotResurrectDiskRemote(t *testing.T) {
	svc, store, dir := newTestBrainService(t)
	ctx := context.Background()
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "original", Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, saved.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Historical DB-only remote edits can disagree with the file. The status
	// patch's durable sync reindexes that file BEFORE merging the metadata.
	data = []byte(strings.Replace(string(data), "type: task", "type: task\ngit_remote: https://unsupported.invalid/o/r", 1))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetNoteByPath(ctx, saved.Path)
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "pending"})
	if err == nil || !strings.Contains(err.Error(), "unsupported.invalid") {
		t.Fatalf("metadata resurrected forbidden disk remote: %v", err)
	}
	after, _ := store.GetNoteByPath(ctx, saved.Path)
	fileAfter, _ := os.ReadFile(path)
	if string(fileAfter) != string(data) || before.Metadata != after.Metadata {
		t.Fatal("failed metadata preflight mutated file/index")
	}
}

func TestGitRemoteUpdateChecksEffectivePrior(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MergeMetadata(ctx, saved.Path, map[string]interface{}{"git_remote": "https://unsupported.invalid/o/r"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{ExecutionMode: strPtr("current_branch"), TargetWorkdir: strPtr("/local/repo")})
	if err == nil {
		t.Fatal("execution update bypassed effective stored remote")
	}
	// An explicit clear through the full-file Update remains a repair path.
	if _, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{GitRemote: strPtr("")}); err != nil {
		t.Fatal(err)
	}
}
