package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// FeatureDeliveryTarget is the single authoritative resolution of where and
// how a completed feature is delivered. Every field is folded from the
// feature's non-generated task entries; disagreements are errors, not guesses.
type FeatureDeliveryTarget struct {
	Workdir            string // target_workdir wins over workdir
	GitRemote          string // normalized; "" means "derive from origin on the runner"
	Provider           string // "gitlab" | "github" | "unknown"
	SourceBranch       string
	TargetBranch       string
	MergeStrategy      string // squash|merge|rebase (default squash)
	RemoteBranchPolicy string // keep|delete (default delete)
	DeliveryMode       string // none|mr|local_merge (folded effective mode)
}

// ResolveFeatureDelivery folds the feature's non-generated task entries into a
// single delivery target. Conflicts (disagreeing workdir/remote/source/target/
// strategy/remote-policy, or a delivery_mode of "conflict"/mr-vs-local_merge)
// return an error whose message names the exact conflict, so the delivery task
// blocks with an actionable note instead of pushing to the wrong place.
func (s *BrainServiceImpl) ResolveFeatureDelivery(ctx context.Context, project, featureID string) (*FeatureDeliveryTarget, error) {
	if s == nil {
		return nil, fmt.Errorf("resolve feature delivery: nil service")
	}
	resp, err := s.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   project,
		FeatureID: featureID,
		Limit:     100,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve feature delivery: list feature %q tasks: %w", featureID, err)
	}
	var entries []types.BrainEntry
	if resp != nil {
		entries = resp.Entries
	}
	return resolveFeatureDeliveryFromEntries(featureID, entries)
}

// resolveFeatureDeliveryFromEntries is the pure fold/conflict core of
// ResolveFeatureDelivery. It takes the feature's task entries already in hand
// so it can be unit-tested without wiring a full service + store.
//
// Generated tasks are skipped (entry.Generated == true): a prior automation's
// task carries the very values we are computing, so including it would let a
// bad value propagate. Every scalar field collects distinct non-empty values
// and errors on disagreement; unset fields fall back to deterministic defaults.
func resolveFeatureDeliveryFromEntries(featureID string, entries []types.BrainEntry) (*FeatureDeliveryTarget, error) {
	var (
		targetWorkdir string
		workdir       string
		gitRemote     string
		sourceBranch  string
		targetBranch  string
		mergeStrategy string
		remotePolicy  string
		sawMR         bool
		sawLocal      bool
	)

	set := func(dst *string, val, label string) error {
		val = strings.TrimSpace(val)
		if val == "" {
			return nil
		}
		if *dst == "" {
			*dst = val
			return nil
		}
		if *dst != val {
			return fmt.Errorf("feature %s: conflicting %s %q vs %q", featureID, label, *dst, val)
		}
		return nil
	}

	for _, e := range entries {
		if e.Generated != nil && *e.Generated {
			continue
		}
		if err := set(&targetWorkdir, e.TargetWorkdir, "target_workdir"); err != nil {
			return nil, err
		}
		if err := set(&workdir, e.Workdir, "workdir"); err != nil {
			return nil, err
		}
		if err := set(&gitRemote, e.GitRemote, "git_remote"); err != nil {
			return nil, err
		}
		if err := set(&sourceBranch, e.GitBranch, "git_branch (source)"); err != nil {
			return nil, err
		}
		if err := set(&targetBranch, e.MergeTargetBranch, "merge_target_branch"); err != nil {
			return nil, err
		}
		if err := set(&mergeStrategy, e.MergeStrategy, "merge_strategy"); err != nil {
			return nil, err
		}
		if err := set(&remotePolicy, e.RemoteBranchPolicy, "remote_branch_policy"); err != nil {
			return nil, err
		}
		switch types.EffectiveDeliveryMode(e.DeliveryMode, e.MergePolicy) {
		case "mr":
			sawMR = true
		case "local_merge":
			sawLocal = true
		}
	}

	if sawMR && sawLocal {
		return nil, fmt.Errorf("feature %s: conflicting delivery_mode mr vs local_merge — set delivery_mode explicitly", featureID)
	}

	// Workdir: target_workdir wins over workdir.
	resolvedWorkdir := firstNonEmpty(targetWorkdir, workdir)

	// Source branch defaults to the feature id; target branch defaults to main.
	if sourceBranch == "" {
		sourceBranch = featureID
	}
	if targetBranch == "" {
		targetBranch = "main"
	}
	if mergeStrategy == "" {
		mergeStrategy = "squash"
	}
	if remotePolicy == "" {
		remotePolicy = "delete"
	}

	deliveryMode := "none"
	switch {
	case sawMR:
		deliveryMode = "mr"
	case sawLocal:
		deliveryMode = "local_merge"
	}

	return &FeatureDeliveryTarget{
		Workdir:            resolvedWorkdir,
		GitRemote:          gitRemote,
		Provider:           providerFromRemote(gitRemote),
		SourceBranch:       sourceBranch,
		TargetBranch:       targetBranch,
		MergeStrategy:      mergeStrategy,
		RemoteBranchPolicy: remotePolicy,
		DeliveryMode:       deliveryMode,
	}, nil
}

// providerFromRemote does a lowercased substring match on the git remote host
// to pick the delivery provider. "" (no remote) leaves the runner to finalize
// from origin, so it returns "unknown".
func providerFromRemote(remote string) string {
	r := strings.ToLower(strings.TrimSpace(remote))
	switch {
	case r == "":
		return "unknown"
	case strings.Contains(r, "gitlab"):
		return "gitlab"
	case strings.Contains(r, "github"):
		return "github"
	default:
		return "unknown"
	}
}
