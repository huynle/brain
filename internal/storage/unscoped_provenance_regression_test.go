package storage

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const p4ReviewPin = "014d3d1d5f0a62ef210fab83a10760ef55094c41"
const mainReviewPin = "cd22b4bdc3b5229621169fe5b214d7ffd12a6015"

// Overlay actual checkout bytes without modifying the pending merge or its index.
type provenanceOverlay struct {
	fs.FS
	files fstest.MapFS
}

func (o provenanceOverlay) Open(name string) (fs.File, error) {
	if _, ok := o.files[name]; ok {
		return o.files.Open(name)
	}
	return o.FS.Open(name)
}

func TestReviewedTwoSourceBaseline(t *testing.T) {
	for _, pin := range []string{p4ReviewPin, mainReviewPin} {
		t.Run(pin[:8], func(t *testing.T) {
			if err := checkUnscopedBaseline("../..", pin); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReviewedProvenanceMutations(t *testing.T) {
	for _, tc := range []struct{ name, file, old, replacement string }{
		{"call argument", "internal/service/resume_with_context.go", "s.storage.MergeMetadata(ctx, task.Path, meta)", "s.storage.MergeMetadata(ctx, taskID, meta)"},
		{"call receiver", "internal/service/resume_with_context.go", "s.storage.GetClaim(ctx, projectID, taskID)", "other.GetClaim(ctx, projectID, taskID)"},
		{"new site", "internal/service/resume_with_context.go", "now := time.Now()", "s.storage.GetClaim(ctx, projectID, taskID); now := time.Now()"},
		{"raw DB escape", "internal/service/resume_with_context.go", "now := time.Now()", "_ = s.storage.DB(); now := time.Now()"},
		{"task raw holder", "internal/service/task.go", "storage      *storage.TenantStore", "storage      *storage.StorageLayer"},
		{"registry raw holder", "internal/service/runner_registry.go", "storage *storage.TenantStore", "storage *storage.StorageLayer"},
		{"shadow storage import", "internal/service/runner_registry.go", `"github.com/huynle/brain-api/internal/storage"`, `"example.invalid/storage"`},
		{"delegate change", "internal/service/runner_registry.go", "s.storage.ListAllInstances(ctx)", "other.ListAllInstances(ctx)"},
		{"attachment argument", "internal/service/attachments.go", "s.storage.GetAttachmentByDigest(ctx, digest)", "s.storage.GetAttachmentByDigest(ctx, req.SHA256)"},
		{"attachment raw holder", "internal/service/attachments.go", "storage      *storage.TenantStore", "storage      *storage.StorageLayer"},
		{"scheduler receiver", "internal/service/scheduler.go", "return s.runners.ListRunners(ctx)", "return other.ListRunners(ctx)"},
		{"scheduler adapter", "internal/service/scheduler.go", "a.source.ListRunners(ctx)", "other.ListRunners(ctx)"},
		{"injector receiver", "internal/apiserver/live_injector.go", "i.instances.ListAllInstances(ctx)", "other.ListAllInstances(ctx)"},
		{"unattested raw escape", "internal/service/resume_with_context.go", `best := ""`, `other.DB(); best := ""`},
		{"unattested inherited spelling", "internal/service/resume_with_context.go", `best := ""`, `other.ListAllInstances(); best := ""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("../../" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.old) {
				t.Fatalf("mutation target missing: %q", tc.old)
			}
			head := provenanceOverlay{os.DirFS("../.."), fstest.MapFS{tc.file: {Data: []byte(strings.Replace(string(data), tc.old, tc.replacement, 1))}}}
			for _, pin := range []string{p4ReviewPin, mainReviewPin} {
				err := checkUnscopedBaselineFS("../..", pin, head)
				if err == nil {
					t.Fatalf("%s admitted mutation against %s", tc.name, pin)
				}
				want := "attestation changed"
				if strings.HasPrefix(tc.name, "unattested") {
					want = "sites source"
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("mutation failed for unrelated reason: want %q, got %v", want, err)
				}
			}
		})
	}
}
