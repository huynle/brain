package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// FeatureScheduleFields holds the feature-level schedule fields extracted from a request.
type FeatureScheduleFields struct {
	Schedule  string
	RunOnceAt string
	StartsAt  string
	ExpiresAt string
	Timezone  string
}

// HasAny returns true if any feature schedule field is set.
func (f FeatureScheduleFields) HasAny() bool {
	return f.Schedule != "" || f.RunOnceAt != "" || f.StartsAt != "" || f.ExpiresAt != ""
}

// extractFeatureScheduleFromCreate extracts feature schedule fields from a CreateEntryRequest.
func extractFeatureScheduleFromCreate(req types.CreateEntryRequest) FeatureScheduleFields {
	return FeatureScheduleFields{
		Schedule:  req.FeatureSchedule,
		RunOnceAt: req.FeatureRunOnceAt,
		StartsAt:  req.FeatureStartsAt,
		ExpiresAt: req.FeatureExpiresAt,
		Timezone:  req.FeatureTimezone,
	}
}

// extractFeatureScheduleFromUpdate extracts feature schedule fields from an UpdateEntryRequest.
// Returns the fields and whether any were provided (non-nil).
func extractFeatureScheduleFromUpdate(req types.UpdateEntryRequest) (FeatureScheduleFields, bool) {
	var fields FeatureScheduleFields
	any := false

	if req.FeatureSchedule != nil {
		fields.Schedule = *req.FeatureSchedule
		any = true
	}
	if req.FeatureRunOnceAt != nil {
		fields.RunOnceAt = *req.FeatureRunOnceAt
		any = true
	}
	if req.FeatureStartsAt != nil {
		fields.StartsAt = *req.FeatureStartsAt
		any = true
	}
	if req.FeatureExpiresAt != nil {
		fields.ExpiresAt = *req.FeatureExpiresAt
		any = true
	}
	if req.FeatureTimezone != nil {
		fields.Timezone = *req.FeatureTimezone
		any = true
	}

	return fields, any
}

// featureScheduleGeneratedKey returns the generated_key for a feature schedule gate task.
func featureScheduleGeneratedKey(featureID string) string {
	return "feature-schedule:" + featureID
}

// ensureFeatureScheduleGate creates or updates a feature_schedule gate task for the given feature.
// The gate task blocks all non-generated tasks in the feature via depends_on injection.
func (s *BrainServiceImpl) ensureFeatureScheduleGate(ctx context.Context, project, featureID string, fields FeatureScheduleFields) error {
	if featureID == "" || !fields.HasAny() {
		return nil
	}

	generatedKey := featureScheduleGeneratedKey(featureID)
	taskDir := filepath.Join(s.config.BrainDir, "projects", project, "task")

	// Look for existing gate task
	existingGate, err := findGeneratedTaskByKey(taskDir, generatedKey)
	if err == nil && existingGate != nil {
		// Gate exists — update its schedule fields
		return s.updateFeatureScheduleGate(ctx, existingGate.ID, fields)
	}

	// No existing gate — create one
	return s.createFeatureScheduleGate(ctx, project, featureID, fields)
}

// createFeatureScheduleGate creates a new feature_schedule gate task.
func (s *BrainServiceImpl) createFeatureScheduleGate(ctx context.Context, project, featureID string, fields FeatureScheduleFields) error {
	generatedKey := featureScheduleGeneratedKey(featureID)
	trueVal := true
	scheduleEnabled := true

	title := fmt.Sprintf("Feature Schedule: %s", featureID)

	gateReq := types.CreateEntryRequest{
		Type:    "task",
		Title:   title,
		Content: fmt.Sprintf("## Feature Schedule Gate\n\nFeature: %s\nProject: %s\n\nThis is an auto-generated gate task that blocks all feature tasks until the scheduled time arrives. The runner's scheduler will complete this task when the schedule triggers.", featureID, project),
		Status:  "active",

		// Schedule fields (copied from feature_* fields)
		Schedule:        fields.Schedule,
		RunOnceAt:       fields.RunOnceAt,
		StartsAt:        fields.StartsAt,
		ExpiresAt:       fields.ExpiresAt,
		ScheduleEnabled: &scheduleEnabled,

		// Generated metadata
		Generated:     &trueVal,
		GeneratedKind: "feature_schedule",
		GeneratedKey:  generatedKey,
		GeneratedBy:   "feature-schedule",

		// Feature association
		FeatureID:      featureID,
		Project:        project,
		CompleteOnIdle: &trueVal,
	}

	gateResp, err := s.Save(ctx, gateReq)
	if err != nil {
		return fmt.Errorf("create feature schedule gate: %w", err)
	}

	slog.Info("created feature schedule gate task",
		"gate_id", gateResp.ID,
		"feature_id", featureID,
		"project", project,
	)

	// Inject the gate task ID into depends_on of all non-generated feature tasks
	return s.injectGateDependency(ctx, project, featureID, gateResp.ID)
}

// updateFeatureScheduleGate updates the schedule fields on an existing gate task.
func (s *BrainServiceImpl) updateFeatureScheduleGate(ctx context.Context, gateID string, fields FeatureScheduleFields) error {
	updateReq := types.UpdateEntryRequest{}

	if fields.Schedule != "" {
		updateReq.Schedule = &fields.Schedule
	}
	if fields.RunOnceAt != "" {
		updateReq.RunOnceAt = &fields.RunOnceAt
	}
	if fields.StartsAt != "" {
		updateReq.StartsAt = &fields.StartsAt
	}
	if fields.ExpiresAt != "" {
		updateReq.ExpiresAt = &fields.ExpiresAt
	}

	_, err := s.Update(ctx, gateID, updateReq)
	if err != nil {
		return fmt.Errorf("update feature schedule gate %s: %w", gateID, err)
	}

	slog.Info("updated feature schedule gate task",
		"gate_id", gateID,
	)

	return nil
}

// injectGateDependency adds the gate task ID to depends_on of all non-generated tasks in the feature.
func (s *BrainServiceImpl) injectGateDependency(ctx context.Context, project, featureID, gateID string) error {
	taskDir := filepath.Join(s.config.BrainDir, "projects", project, "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read task dir: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		// Skip tasks not in this feature
		if doc.Frontmatter.FeatureID != featureID {
			continue
		}

		// Skip generated tasks (including the gate itself)
		if doc.Frontmatter.Generated != nil && *doc.Frontmatter.Generated {
			continue
		}

		shortID := strings.TrimSuffix(entry.Name(), ".md")

		// Check if gate is already in depends_on
		alreadyHasGate := false
		for _, dep := range doc.Frontmatter.DependsOn {
			if dep == gateID {
				alreadyHasGate = true
				break
			}
		}

		if alreadyHasGate {
			continue
		}

		// Add gate to depends_on
		newDeps := append(doc.Frontmatter.DependsOn, gateID)
		updateReq := types.UpdateEntryRequest{
			DependsOn: &newDeps,
		}

		if _, err := s.Update(ctx, shortID, updateReq); err != nil {
			slog.Warn("failed to inject gate dependency",
				"task_id", shortID,
				"gate_id", gateID,
				"error", err,
			)
			continue
		}

		slog.Debug("injected gate dependency",
			"task_id", shortID,
			"gate_id", gateID,
		)
	}

	return nil
}

// findGeneratedTaskByKey finds a generated task by its generated_key.
// Returns the task info if found, or nil if not found.
func findGeneratedTaskByKey(taskDir, generatedKey string) (*types.CreateEntryResponse, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		if doc.Frontmatter.GeneratedKey == generatedKey {
			shortID := strings.TrimSuffix(entry.Name(), ".md")
			return &types.CreateEntryResponse{
				ID:     shortID,
				Path:   fmt.Sprintf("projects/%s/task/%s", filepath.Base(filepath.Dir(taskDir)), entry.Name()),
				Title:  doc.Frontmatter.Title,
				Type:   "task",
				Status: doc.Frontmatter.Status,
			}, nil
		}
	}

	return nil, fmt.Errorf("not found")
}
