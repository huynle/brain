package service

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestResolveFeatureDeliveryFromEntries_HappyPath(t *testing.T) {
	entries := []types.BrainEntry{
		{
			ID:                "t1",
			TargetWorkdir:     "/work/repo",
			GitRemote:         "git@gitlab.example.com:group/proj.git",
			GitBranch:         "feature/x",
			MergeTargetBranch: "develop",
			MergeStrategy:     "squash",
			DeliveryMode:      "mr",
		},
		{
			ID:            "t2",
			TargetWorkdir: "/work/repo",
			DeliveryMode:  "mr",
		},
	}

	got, err := resolveFeatureDeliveryFromEntries("feat-x", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Workdir != "/work/repo" {
		t.Errorf("Workdir = %q, want %q", got.Workdir, "/work/repo")
	}
	if got.GitRemote != "git@gitlab.example.com:group/proj.git" {
		t.Errorf("GitRemote = %q", got.GitRemote)
	}
	if got.Provider != "gitlab" {
		t.Errorf("Provider = %q, want gitlab", got.Provider)
	}
	if got.SourceBranch != "feature/x" {
		t.Errorf("SourceBranch = %q, want feature/x", got.SourceBranch)
	}
	if got.TargetBranch != "develop" {
		t.Errorf("TargetBranch = %q, want develop", got.TargetBranch)
	}
	if got.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want squash", got.MergeStrategy)
	}
	if got.RemoteBranchPolicy != "delete" {
		t.Errorf("RemoteBranchPolicy = %q, want delete (default)", got.RemoteBranchPolicy)
	}
	if got.DeliveryMode != "mr" {
		t.Errorf("DeliveryMode = %q, want mr", got.DeliveryMode)
	}
}

func TestResolveFeatureDeliveryFromEntries_Defaults(t *testing.T) {
	// A single bare entry: everything falls back to defaults.
	entries := []types.BrainEntry{
		{ID: "t1"},
	}
	got, err := resolveFeatureDeliveryFromEntries("feat-defaults", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SourceBranch != "feat-defaults" {
		t.Errorf("SourceBranch = %q, want feat-defaults (defaults to featureID)", got.SourceBranch)
	}
	if got.TargetBranch != "main" {
		t.Errorf("TargetBranch = %q, want main (default)", got.TargetBranch)
	}
	if got.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want squash (default)", got.MergeStrategy)
	}
	if got.RemoteBranchPolicy != "delete" {
		t.Errorf("RemoteBranchPolicy = %q, want delete (default)", got.RemoteBranchPolicy)
	}
	if got.GitRemote != "" {
		t.Errorf("GitRemote = %q, want empty (runner derives from origin)", got.GitRemote)
	}
	if got.Provider != "unknown" {
		t.Errorf("Provider = %q, want unknown (no remote)", got.Provider)
	}
	if got.DeliveryMode != "none" {
		t.Errorf("DeliveryMode = %q, want none (default)", got.DeliveryMode)
	}
}

func TestResolveFeatureDeliveryFromEntries_WorkdirFallback(t *testing.T) {
	// target_workdir wins over workdir.
	entries := []types.BrainEntry{
		{ID: "t1", Workdir: "/plain/work"},
		{ID: "t2", TargetWorkdir: "/target/work", Workdir: "/plain/work"},
	}
	got, err := resolveFeatureDeliveryFromEntries("feat-wd", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Workdir != "/target/work" {
		t.Errorf("Workdir = %q, want /target/work (target_workdir wins)", got.Workdir)
	}
}

func TestResolveFeatureDeliveryFromEntries_WorkdirOnly(t *testing.T) {
	entries := []types.BrainEntry{
		{ID: "t1", Workdir: "/plain/work"},
	}
	got, err := resolveFeatureDeliveryFromEntries("feat-wd2", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Workdir != "/plain/work" {
		t.Errorf("Workdir = %q, want /plain/work", got.Workdir)
	}
}

func TestResolveFeatureDeliveryFromEntries_SkipsGenerated(t *testing.T) {
	// A generated task carries a bad workdir; it must be ignored.
	entries := []types.BrainEntry{
		{ID: "gen", Generated: boolPtr(true), TargetWorkdir: "/tmp/bad", GitRemote: "git@github.com:x/y.git"},
		{ID: "t1", TargetWorkdir: "/good/work"},
	}
	got, err := resolveFeatureDeliveryFromEntries("feat-gen", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Workdir != "/good/work" {
		t.Errorf("Workdir = %q, want /good/work (generated skipped)", got.Workdir)
	}
	if got.GitRemote != "" {
		t.Errorf("GitRemote = %q, want empty (generated remote skipped)", got.GitRemote)
	}
}

func TestResolveFeatureDeliveryFromEntries_ProviderDetection(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"git@gitlab.example.com:g/p.git", "gitlab"},
		{"https://github.com/o/r.git", "github"},
		{"git@bitbucket.org:o/r.git", "unknown"},
	}
	for _, tt := range tests {
		entries := []types.BrainEntry{{ID: "t1", GitRemote: tt.remote}}
		got, err := resolveFeatureDeliveryFromEntries("feat-prov", entries)
		if err != nil {
			t.Fatalf("remote %q: unexpected error: %v", tt.remote, err)
		}
		if got.Provider != tt.want {
			t.Errorf("remote %q: Provider = %q, want %q", tt.remote, got.Provider, tt.want)
		}
	}
}

func TestResolveFeatureDeliveryFromEntries_Conflicts(t *testing.T) {
	tests := []struct {
		name     string
		entries  []types.BrainEntry
		contains string
	}{
		{
			name: "target_workdir conflict",
			entries: []types.BrainEntry{
				{ID: "t1", TargetWorkdir: "/a"},
				{ID: "t2", TargetWorkdir: "/b"},
			},
			contains: "conflicting target_workdir",
		},
		{
			name: "workdir conflict",
			entries: []types.BrainEntry{
				{ID: "t1", Workdir: "/a"},
				{ID: "t2", Workdir: "/b"},
			},
			contains: "conflicting workdir",
		},
		{
			name: "git_remote conflict",
			entries: []types.BrainEntry{
				{ID: "t1", GitRemote: "git@gitlab.example.com:g/a.git"},
				{ID: "t2", GitRemote: "git@gitlab.example.com:g/b.git"},
			},
			contains: "conflicting git_remote",
		},
		{
			name: "git_branch (source) conflict",
			entries: []types.BrainEntry{
				{ID: "t1", GitBranch: "feat/a"},
				{ID: "t2", GitBranch: "feat/b"},
			},
			contains: "conflicting git_branch",
		},
		{
			name: "merge_target_branch conflict",
			entries: []types.BrainEntry{
				{ID: "t1", MergeTargetBranch: "main"},
				{ID: "t2", MergeTargetBranch: "develop"},
			},
			contains: "conflicting merge_target_branch",
		},
		{
			name: "merge_strategy conflict",
			entries: []types.BrainEntry{
				{ID: "t1", MergeStrategy: "squash"},
				{ID: "t2", MergeStrategy: "rebase"},
			},
			contains: "conflicting merge_strategy",
		},
		{
			name: "remote_branch_policy conflict",
			entries: []types.BrainEntry{
				{ID: "t1", RemoteBranchPolicy: "keep"},
				{ID: "t2", RemoteBranchPolicy: "delete"},
			},
			contains: "conflicting remote_branch_policy",
		},
		{
			name: "delivery_mode conflict mr vs local_merge",
			entries: []types.BrainEntry{
				{ID: "t1", DeliveryMode: "mr"},
				{ID: "t2", DeliveryMode: "local_merge"},
			},
			contains: "conflicting delivery_mode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveFeatureDeliveryFromEntries("feat-conflict", tt.entries)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.contains)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.contains)
			}
			if !strings.Contains(err.Error(), "feat-conflict") {
				t.Fatalf("error = %q, want it to name the feature id", err.Error())
			}
		})
	}
}

// TestResolveFeatureDeliveryFromEntries_ProviderMismatchErrors covers the task's
// "conflicting git_remote (github vs gitlab) -> clear error, no silent guess"
// case explicitly. Two entries whose remotes point at DIFFERENT forges must
// error out naming git_remote rather than silently picking one — a silent pick
// would push a completed feature at the wrong provider entirely.
//
// GitLab-only constraint: the github remote here is a literal string compared
// in-process; nothing dials github.com. The gitlab side uses gitlab.us.lmco.com.
func TestResolveFeatureDeliveryFromEntries_ProviderMismatchErrors(t *testing.T) {
	entries := []types.BrainEntry{
		{ID: "t1", GitRemote: "git@gitlab.us.lmco.com:orion/ai/canis.git"},
		{ID: "t2", GitRemote: "https://github.com/huynle/brain.git"},
	}
	_, err := resolveFeatureDeliveryFromEntries("feat-provmix", entries)
	if err == nil {
		t.Fatal("expected an error for github-vs-gitlab remote mismatch, got nil (silent guess)")
	}
	if !strings.Contains(err.Error(), "conflicting git_remote") {
		t.Fatalf("error = %q, want it to name conflicting git_remote", err.Error())
	}
	if !strings.Contains(err.Error(), "feat-provmix") {
		t.Fatalf("error = %q, want it to name the feature id", err.Error())
	}
}

// TestResolveFeatureDeliveryFromEntries_DisagreeingBranchNamesError covers the
// "disagreeing branch names -> clear error" half of the git-resolution case for
// both the source branch (git_branch) and the target branch, since a wrong
// guess on either is a delivery to the wrong ref.
func TestResolveFeatureDeliveryFromEntries_DisagreeingBranchNamesError(t *testing.T) {
	t.Run("source branch disagreement", func(t *testing.T) {
		entries := []types.BrainEntry{
			{ID: "t1", GitBranch: "feature/a"},
			{ID: "t2", GitBranch: "feature/b"},
		}
		_, err := resolveFeatureDeliveryFromEntries("feat-src", entries)
		if err == nil {
			t.Fatal("expected error for disagreeing source branch, got nil")
		}
		if !strings.Contains(err.Error(), "git_branch") {
			t.Fatalf("error = %q, want it to name git_branch (source)", err.Error())
		}
	})
	t.Run("target branch disagreement", func(t *testing.T) {
		entries := []types.BrainEntry{
			{ID: "t1", MergeTargetBranch: "main"},
			{ID: "t2", MergeTargetBranch: "release"},
		}
		_, err := resolveFeatureDeliveryFromEntries("feat-tgt", entries)
		if err == nil {
			t.Fatal("expected error for disagreeing target branch, got nil")
		}
		if !strings.Contains(err.Error(), "merge_target_branch") {
			t.Fatalf("error = %q, want it to name merge_target_branch", err.Error())
		}
	})
}

// TestResolveFeatureDeliveryFromEntries_AgreeingGitlabRemotesNoError guards the
// inverse of the mismatch case: two entries carrying the SAME gitlab remote are
// a single source of truth and must resolve cleanly to that remote + gitlab
// provider, never a spurious conflict.
func TestResolveFeatureDeliveryFromEntries_AgreeingGitlabRemotesNoError(t *testing.T) {
	const remote = "git@gitlab.us.lmco.com:orion/ai/canis.git"
	entries := []types.BrainEntry{
		{ID: "t1", GitRemote: remote, DeliveryMode: "mr"},
		{ID: "t2", GitRemote: remote},
	}
	got, err := resolveFeatureDeliveryFromEntries("feat-agree", entries)
	if err != nil {
		t.Fatalf("unexpected error for agreeing gitlab remotes: %v", err)
	}
	if got.GitRemote != remote {
		t.Errorf("GitRemote = %q, want %q", got.GitRemote, remote)
	}
	if got.Provider != "gitlab" {
		t.Errorf("Provider = %q, want gitlab", got.Provider)
	}
}
