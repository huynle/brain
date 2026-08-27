package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

const BrainMergeRequestGeneratedBy = "brain:merge-request"

type BrainMergeRequestConfig struct {
	Project            string
	FeatureID          string
	SourceBranch       string
	TargetBranch       string
	MergePolicy        string
	MergeStrategy      string
	RemoteBranchPolicy string
	TargetWorkdir      string
	CoverageReport     string
}

type BrainMergeRequestResult struct {
	Created bool
	Entry   *types.BrainEntry
}

func (s *BrainServiceImpl) EnsureBrainMergeRequest(ctx context.Context, cfg BrainMergeRequestConfig) (*BrainMergeRequestResult, error) {
	if strings.TrimSpace(cfg.Project) == "" {
		return nil, fmt.Errorf("project is required")
	}
	if strings.TrimSpace(cfg.FeatureID) == "" {
		return nil, fmt.Errorf("feature_id is required")
	}

	sourceBranch := firstNonEmpty(cfg.SourceBranch, cfg.FeatureID)
	targetBranch := firstNonEmpty(cfg.TargetBranch, "main")
	mergePolicy := firstNonEmpty(cfg.MergePolicy, "auto_pr")
	mergeStrategy := firstNonEmpty(cfg.MergeStrategy, "squash")
	remoteBranchPolicy := firstNonEmpty(cfg.RemoteBranchPolicy, "delete")
	generatedKey := brainMergeRequestGeneratedKey(cfg.Project, cfg.FeatureID, sourceBranch, targetBranch)

	existing, err := s.List(ctx, types.ListEntriesRequest{
		Type:      "merge_request",
		Project:   cfg.Project,
		FeatureID: cfg.FeatureID,
		Limit:     1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list merge requests: %w", err)
	}
	for i := range existing.Entries {
		entry := existing.Entries[i]
		if entry.GeneratedKey == generatedKey {
			return &BrainMergeRequestResult{Created: false, Entry: &entry}, nil
		}
	}

	content := buildBrainMergeRequestContent(cfg, sourceBranch, targetBranch, mergePolicy, mergeStrategy, remoteBranchPolicy)
	resp, err := s.Save(ctx, types.CreateEntryRequest{
		Type:               "merge_request",
		Title:              fmt.Sprintf("Merge request: %s -> %s", sourceBranch, targetBranch),
		Content:            content,
		Status:             "pending",
		Project:            cfg.Project,
		FeatureID:          cfg.FeatureID,
		GitBranch:          sourceBranch,
		MergeTargetBranch:  targetBranch,
		MergePolicy:        mergePolicy,
		MergeStrategy:      mergeStrategy,
		RemoteBranchPolicy: remoteBranchPolicy,
		TargetWorkdir:      cfg.TargetWorkdir,
		Generated:          serviceBoolPtr(true),
		GeneratedKind:      "other",
		GeneratedKey:       generatedKey,
		GeneratedBy:        BrainMergeRequestGeneratedBy,
		Tags:               []string{"merge-request", cfg.FeatureID},
	})
	if err != nil {
		return nil, fmt.Errorf("create merge request: %w", err)
	}

	entry, err := s.Recall(ctx, resp.Path)
	if err != nil {
		return nil, fmt.Errorf("recall merge request: %w", err)
	}
	return &BrainMergeRequestResult{Created: true, Entry: entry}, nil
}

func brainMergeRequestGeneratedKey(project, featureID, sourceBranch, targetBranch string) string {
	return fmt.Sprintf("merge-request:%s:%s:%s:%s", project, featureID, sourceBranch, targetBranch)
}

func buildBrainMergeRequestContent(cfg BrainMergeRequestConfig, sourceBranch, targetBranch, mergePolicy, mergeStrategy, remoteBranchPolicy string) string {
	var b strings.Builder
	b.WriteString("## Brain Merge Request\n\n")
	fmt.Fprintf(&b, "- feature_id: %s\n", cfg.FeatureID)
	fmt.Fprintf(&b, "- source_branch: %s\n", sourceBranch)
	fmt.Fprintf(&b, "- target_branch: %s\n", targetBranch)
	fmt.Fprintf(&b, "- merge_policy: %s\n", mergePolicy)
	fmt.Fprintf(&b, "- merge_strategy: %s\n", mergeStrategy)
	fmt.Fprintf(&b, "- remote_branch_policy: %s\n", remoteBranchPolicy)
	if cfg.TargetWorkdir != "" {
		fmt.Fprintf(&b, "- target_workdir: %s\n", cfg.TargetWorkdir)
	}
	if strings.TrimSpace(cfg.CoverageReport) != "" {
		b.WriteString("\n## Checkout Coverage\n\n")
		b.WriteString(strings.TrimSpace(cfg.CoverageReport))
		b.WriteString("\n")
	}
	return b.String()
}
