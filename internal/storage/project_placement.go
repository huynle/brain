package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ProjectPlacementRow stores Brain-owned scheduling placement policy for a project.
type ProjectPlacementRow struct {
	ProjectID            string
	Affinity             string
	PreferredMachines    []string
	AllowedMachines      []string
	WorkspacePolicy      string
	RequiredLabels       map[string]string
	RequiredCapabilities []string
	ResourceRequirements map[string]any
}

// DefaultProjectPlacement returns the policy used when a project has no stored row.
func DefaultProjectPlacement(projectID string) *ProjectPlacementRow {
	return &ProjectPlacementRow{
		ProjectID:            projectID,
		Affinity:             "soft",
		PreferredMachines:    []string{},
		AllowedMachines:      []string{},
		WorkspacePolicy:      "",
		RequiredLabels:       map[string]string{},
		RequiredCapabilities: []string{},
		ResourceRequirements: map[string]any{},
	}
}

func (s *StorageLayer) GetProjectPlacement(ctx context.Context, projectID string) (*ProjectPlacementRow, error) {
	var row ProjectPlacementRow
	var preferredJSON, allowedJSON, labelsJSON, capabilitiesJSON, resourcesJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT project_id, affinity, preferred_machines, allowed_machines, workspace_policy,
		       required_labels, required_capabilities, resource_requirements
		FROM project_placement WHERE project_id = ?`, projectID).Scan(
		&row.ProjectID, &row.Affinity, &preferredJSON, &allowedJSON, &row.WorkspacePolicy,
		&labelsJSON, &capabilitiesJSON, &resourcesJSON,
	)
	if err == sql.ErrNoRows {
		return DefaultProjectPlacement(projectID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project placement: %w", err)
	}
	if err := json.Unmarshal([]byte(preferredJSON), &row.PreferredMachines); err != nil {
		return nil, fmt.Errorf("unmarshal preferred machines: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedJSON), &row.AllowedMachines); err != nil {
		return nil, fmt.Errorf("unmarshal allowed machines: %w", err)
	}
	if err := json.Unmarshal([]byte(labelsJSON), &row.RequiredLabels); err != nil {
		return nil, fmt.Errorf("unmarshal required labels: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &row.RequiredCapabilities); err != nil {
		return nil, fmt.Errorf("unmarshal required capabilities: %w", err)
	}
	if err := json.Unmarshal([]byte(resourcesJSON), &row.ResourceRequirements); err != nil {
		return nil, fmt.Errorf("unmarshal resource requirements: %w", err)
	}
	return &row, nil
}

func (s *StorageLayer) UpsertProjectPlacement(ctx context.Context, row *ProjectPlacementRow) error {
	if row == nil {
		return fmt.Errorf("project placement row is nil")
	}
	if row.Affinity == "" {
		row.Affinity = "soft"
	}
	preferredJSON, err := json.Marshal(nonNilStrings(row.PreferredMachines))
	if err != nil {
		return fmt.Errorf("marshal preferred machines: %w", err)
	}
	allowedJSON, err := json.Marshal(nonNilStrings(row.AllowedMachines))
	if err != nil {
		return fmt.Errorf("marshal allowed machines: %w", err)
	}
	labelsJSON, err := json.Marshal(nonNilStringMap(row.RequiredLabels))
	if err != nil {
		return fmt.Errorf("marshal required labels: %w", err)
	}
	capabilitiesJSON, err := json.Marshal(nonNilStrings(row.RequiredCapabilities))
	if err != nil {
		return fmt.Errorf("marshal required capabilities: %w", err)
	}
	resourcesJSON, err := json.Marshal(nonNilAnyMap(row.ResourceRequirements))
	if err != nil {
		return fmt.Errorf("marshal resource requirements: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO project_placement
		  (project_id, affinity, preferred_machines, allowed_machines, workspace_policy,
		   required_labels, required_capabilities, resource_requirements, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(project_id) DO UPDATE SET
		  affinity = excluded.affinity,
		  preferred_machines = excluded.preferred_machines,
		  allowed_machines = excluded.allowed_machines,
		  workspace_policy = excluded.workspace_policy,
		  required_labels = excluded.required_labels,
		  required_capabilities = excluded.required_capabilities,
		  resource_requirements = excluded.resource_requirements,
		  updated_at = datetime('now')`,
		row.ProjectID, row.Affinity, string(preferredJSON), string(allowedJSON), row.WorkspacePolicy,
		string(labelsJSON), string(capabilitiesJSON), string(resourcesJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert project placement: %w", err)
	}
	return nil
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nonNilStringMap(v map[string]string) map[string]string {
	if v == nil {
		return map[string]string{}
	}
	return v
}

func nonNilAnyMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}
