package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestEnsureBrainMergeRequestCreatesMergeRequestEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := brain.EnsureBrainMergeRequest(ctx, BrainMergeRequestConfig{
		Project:            "brain",
		FeatureID:          "feature-default-worktree",
		SourceBranch:       "feature-default-worktree",
		TargetBranch:       "main",
		MergePolicy:        "auto_pr",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
		CoverageReport:     "all criteria covered",
	})
	if err != nil {
		t.Fatalf("EnsureBrainMergeRequest failed: %v", err)
	}
	if !resp.Created {
		t.Fatal("Created = false, want true")
	}
	if resp.Entry == nil {
		t.Fatal("Entry is nil")
	}
	if resp.Entry.Type != "merge_request" {
		t.Fatalf("entry type = %q, want merge_request", resp.Entry.Type)
	}
	if resp.Entry.Status != "pending" {
		t.Fatalf("entry status = %q, want pending", resp.Entry.Status)
	}
	if resp.Entry.FeatureID != "feature-default-worktree" {
		t.Fatalf("entry feature_id = %q, want feature-default-worktree", resp.Entry.FeatureID)
	}
	if resp.Entry.GitBranch != "feature-default-worktree" || resp.Entry.MergeTargetBranch != "main" {
		t.Fatalf("branches = source %q target %q, want feature-default-worktree -> main", resp.Entry.GitBranch, resp.Entry.MergeTargetBranch)
	}
	if resp.Entry.MergePolicy != "auto_pr" || resp.Entry.MergeStrategy != "squash" {
		t.Fatalf("merge intent = %q/%q, want auto_pr/squash", resp.Entry.MergePolicy, resp.Entry.MergeStrategy)
	}
	if !strings.Contains(resp.Entry.Content, "all criteria covered") {
		t.Fatalf("entry content missing coverage report: %s", resp.Entry.Content)
	}
}

func TestEnsureBrainMergeRequestIsIdempotentByFeatureAndTarget(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	cfg := BrainMergeRequestConfig{
		Project:      "brain",
		FeatureID:    "feature-default-worktree",
		SourceBranch: "feature-default-worktree",
		TargetBranch: "main",
	}

	first, err := brain.EnsureBrainMergeRequest(ctx, cfg)
	if err != nil {
		t.Fatalf("first EnsureBrainMergeRequest failed: %v", err)
	}
	second, err := brain.EnsureBrainMergeRequest(ctx, cfg)
	if err != nil {
		t.Fatalf("second EnsureBrainMergeRequest failed: %v", err)
	}
	if !first.Created {
		t.Fatal("first Created = false, want true")
	}
	if second.Created {
		t.Fatal("second Created = true, want false")
	}
	if first.Entry.ID != second.Entry.ID {
		t.Fatalf("idempotent entry IDs differ: %q vs %q", first.Entry.ID, second.Entry.ID)
	}

	list, err := brain.List(ctx, types.ListEntriesRequest{Type: "merge_request", Project: "brain", FeatureID: "feature-default-worktree", Limit: 10})
	if err != nil {
		t.Fatalf("List merge requests failed: %v", err)
	}
	if len(list.Entries) != 1 {
		t.Fatalf("merge request count = %d, want 1", len(list.Entries))
	}
}
