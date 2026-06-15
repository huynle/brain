package service

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ProjectPlacementService validates and persists Brain-owned project placement policy.
type ProjectPlacementService struct {
	store *storage.StorageLayer
}

func NewProjectPlacementService(store *storage.StorageLayer) *ProjectPlacementService {
	return &ProjectPlacementService{store: store}
}

func (s *ProjectPlacementService) Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error) {
	row, err := s.store.GetProjectPlacement(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return placementFromRow(row), nil
}

func (s *ProjectPlacementService) Put(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error) {
	placement.ProjectID = projectID
	if placement.Affinity == "" {
		placement.Affinity = types.PlacementAffinitySoft
	}
	if !validPlacementAffinity(placement.Affinity) {
		return nil, fmt.Errorf("invalid affinity %q", placement.Affinity)
	}
	if placement.WorkspacePolicy != "" && !validWorkspacePolicy(placement.WorkspacePolicy) {
		return nil, fmt.Errorf("invalid workspace_policy %q", placement.WorkspacePolicy)
	}
	row := placementToRow(placement)
	if err := s.store.UpsertProjectPlacement(ctx, row); err != nil {
		return nil, err
	}
	return s.Get(ctx, projectID)
}

func validPlacementAffinity(v string) bool {
	switch v {
	case types.PlacementAffinityStrict, types.PlacementAffinitySoft, types.PlacementAffinityNone:
		return true
	default:
		return false
	}
}

func validWorkspacePolicy(v string) bool {
	switch v {
	case types.WorkspacePolicyWorktree, types.WorkspacePolicyCurrentBranch:
		return true
	default:
		return false
	}
}

func placementFromRow(row *storage.ProjectPlacementRow) *types.ProjectPlacement {
	if row == nil {
		return nil
	}
	return &types.ProjectPlacement{
		ProjectID:            row.ProjectID,
		Affinity:             row.Affinity,
		PreferredMachines:    row.PreferredMachines,
		AllowedMachines:      row.AllowedMachines,
		WorkspacePolicy:      row.WorkspacePolicy,
		RequiredLabels:       row.RequiredLabels,
		RequiredCapabilities: row.RequiredCapabilities,
		Resources:            row.ResourceRequirements,
	}
}

func placementToRow(placement types.ProjectPlacement) *storage.ProjectPlacementRow {
	return &storage.ProjectPlacementRow{
		ProjectID:            placement.ProjectID,
		Affinity:             placement.Affinity,
		PreferredMachines:    placement.PreferredMachines,
		AllowedMachines:      placement.AllowedMachines,
		WorkspacePolicy:      placement.WorkspacePolicy,
		RequiredLabels:       placement.RequiredLabels,
		RequiredCapabilities: placement.RequiredCapabilities,
		ResourceRequirements: placement.Resources,
	}
}
