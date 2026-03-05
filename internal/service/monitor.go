package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// MonitorScope defines the scope for a monitor (all, project, or feature).
type MonitorScope struct {
	Type      string // "all", "project", or "feature"
	Project   string
	FeatureID string
}

// MonitorTagResult holds the parsed result of a monitor tag.
type MonitorTagResult struct {
	TemplateID string
	Scope      MonitorScope
}

// MonitorInfo holds information about an active monitor.
type MonitorInfo struct {
	ID         string
	Path       string
	TemplateID string
	Scope      MonitorScope
	Enabled    bool
	Schedule   string
	Title      string
}

// CreateMonitorResult holds the result of creating a monitor.
type CreateMonitorResult struct {
	ID    string
	Path  string
	Title string
}

// CreateMonitorOptions holds optional parameters for creating a monitor.
type CreateMonitorOptions struct {
	Schedule string
	Project  string
}

// MonitorListFilter holds optional filters for listing monitors.
type MonitorListFilter struct {
	Project    string
	FeatureID  string
	TemplateID string
}

// MonitorFindResult holds the result of finding a monitor.
type MonitorFindResult struct {
	ID       string
	Path     string
	Enabled  bool
	Schedule string
}

// =============================================================================
// Monitor Templates
// =============================================================================

// MonitorTemplate defines a reusable template for recurring monitoring tasks.
type MonitorTemplate struct {
	ID              string
	Label           string
	Description     string
	DefaultSchedule string
	Tags            []string
}

// monitorTemplates is the registry of known monitor templates.
var monitorTemplates = map[string]MonitorTemplate{
	"blocked-inspector": {
		ID:              "blocked-inspector",
		Label:           "Blocked Task Inspector",
		Description:     "Periodically checks for blocked tasks and attempts to unblock them",
		DefaultSchedule: "*/15 * * * *",
		Tags:            []string{"scheduled", "inspector", "monitoring"},
	},
	"feature-review": {
		ID:              "feature-review",
		Label:           "Feature Code Review",
		Description:     "Two-phase review: completeness against original requests + code quality",
		DefaultSchedule: "",
		Tags:            []string{"monitor", "review"},
	},
}

// =============================================================================
// Tag/Title Helpers
// =============================================================================

// describeScopeShort returns a short description of the scope.
func describeScopeShort(scope MonitorScope) string {
	switch scope.Type {
	case "all":
		return "all projects"
	case "project":
		return "project " + scope.Project
	case "feature":
		return "feature " + scope.FeatureID
	default:
		return "unknown"
	}
}

// BuildMonitorTag generates a deterministic tag for monitor lookup.
// e.g., "monitor:blocked-inspector:project:brain-api"
func BuildMonitorTag(templateID string, scope MonitorScope) string {
	switch scope.Type {
	case "all":
		return "monitor:" + templateID + ":all"
	case "project":
		return "monitor:" + templateID + ":project:" + scope.Project
	case "feature":
		return "monitor:" + templateID + ":feature:" + scope.FeatureID + ":" + scope.Project
	default:
		return "monitor:" + templateID + ":unknown"
	}
}

// ParseMonitorTag parses a monitor tag back into templateId + scope.
// Returns nil if the tag doesn't match the expected format.
func ParseMonitorTag(tag string) *MonitorTagResult {
	const prefix = "monitor:"
	if !strings.HasPrefix(tag, prefix) {
		return nil
	}

	rest := tag[len(prefix):]

	// Try "templateId:all"
	if idx := strings.Index(rest, ":all"); idx > 0 && rest[idx:] == ":all" {
		templateID := rest[:idx]
		if templateID == "" {
			return nil
		}
		return &MonitorTagResult{
			TemplateID: templateID,
			Scope:      MonitorScope{Type: "all"},
		}
	}

	// Try "templateId:feature:featureId:projectName"
	// Must check feature before project since "feature" contains more colons
	if featureIdx := strings.Index(rest, ":feature:"); featureIdx > 0 {
		templateID := rest[:featureIdx]
		featurePart := rest[featureIdx+len(":feature:"):]
		colonIdx := strings.Index(featurePart, ":")
		if colonIdx <= 0 {
			return nil
		}
		featureID := featurePart[:colonIdx]
		project := featurePart[colonIdx+1:]
		if featureID == "" || project == "" {
			return nil
		}
		return &MonitorTagResult{
			TemplateID: templateID,
			Scope:      MonitorScope{Type: "feature", FeatureID: featureID, Project: project},
		}
	}

	// Try "templateId:project:projectName"
	if projectIdx := strings.Index(rest, ":project:"); projectIdx > 0 {
		templateID := rest[:projectIdx]
		project := rest[projectIdx+len(":project:"):]
		if project == "" {
			return nil
		}
		return &MonitorTagResult{
			TemplateID: templateID,
			Scope:      MonitorScope{Type: "project", Project: project},
		}
	}

	return nil
}

// BuildMonitorTitle generates a deterministic title for a monitor task.
// e.g., "Monitor: Blocked Task Inspector (project brain-api)"
func BuildMonitorTitle(label string, scope MonitorScope) string {
	scopeLabel := describeScopeShort(scope)
	return fmt.Sprintf("Monitor: %s (%s)", label, scopeLabel)
}

// =============================================================================
// MonitorServiceImpl
// =============================================================================

// MonitorServiceImpl implements monitor operations using a BrainService.
type MonitorServiceImpl struct {
	brain api.BrainService
}

// NewMonitorService creates a new MonitorServiceImpl.
func NewMonitorService(brain api.BrainService) *MonitorServiceImpl {
	return &MonitorServiceImpl{brain: brain}
}

// Create creates a new monitor task from a template.
func (s *MonitorServiceImpl) Create(ctx context.Context, templateID string, scope MonitorScope, opts *CreateMonitorOptions) (*CreateMonitorResult, error) {
	template, ok := monitorTemplates[templateID]
	if !ok {
		return nil, fmt.Errorf("unknown monitor template: %s", templateID)
	}

	// Check for existing monitor
	existing, err := s.Find(ctx, templateID, scope)
	if err != nil {
		return nil, fmt.Errorf("check existing monitor: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("monitor already exists for template %q with this scope (id: %s)", templateID, existing.ID)
	}

	tag := BuildMonitorTag(templateID, scope)
	title := BuildMonitorTitle(template.Label, scope)

	// Determine project
	var project string
	if scope.Type == "project" {
		project = scope.Project
	} else if scope.Type == "feature" {
		project = scope.Project
	} else if opts != nil {
		project = opts.Project
	}

	// Determine schedule
	schedule := template.DefaultSchedule
	if opts != nil && opts.Schedule != "" {
		schedule = opts.Schedule
	}

	schedEnabled := true
	completeOnIdle := true

	tags := make([]string, 0, len(template.Tags)+1)
	tags = append(tags, template.Tags...)
	tags = append(tags, tag)

	result, err := s.brain.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           title,
		Content:         fmt.Sprintf("## Monitor Task\n\nTemplate: %s\nScope: %s\n\nThis task was created from a monitor template.", template.Label, describeScopeShort(scope)),
		Schedule:        schedule,
		ScheduleEnabled: &schedEnabled,
		CompleteOnIdle:  &completeOnIdle,
		ExecutionMode:   "current_branch",
		Tags:            tags,
		FeatureID:       scope.FeatureID,
		Project:         project,
		Status:          "active",
	})
	if err != nil {
		return nil, fmt.Errorf("save monitor entry: %w", err)
	}

	return &CreateMonitorResult{
		ID:    result.ID,
		Path:  result.Path,
		Title: title,
	}, nil
}

// Find finds an existing monitor for a template+scope combo (by tag lookup).
func (s *MonitorServiceImpl) Find(ctx context.Context, templateID string, scope MonitorScope) (*MonitorFindResult, error) {
	tag := BuildMonitorTag(templateID, scope)
	result, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type: "task",
		Tags: tag,
	})
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, nil
	}

	entry := result.Entries[0]
	enabled := entry.ScheduleEnabled == nil || *entry.ScheduleEnabled
	return &MonitorFindResult{
		ID:       entry.ID,
		Path:     entry.Path,
		Enabled:  enabled,
		Schedule: entry.Schedule,
	}, nil
}

// List lists all active monitors, optionally filtered.
func (s *MonitorServiceImpl) List(ctx context.Context, filter *MonitorListFilter) ([]MonitorInfo, error) {
	result, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type: "task",
		Tags: "monitor",
	})
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}

	var monitors []MonitorInfo
	for _, entry := range result.Entries {
		// Find the monitor tag in the entry's tags
		var monitorTag string
		for _, t := range entry.Tags {
			if strings.HasPrefix(t, "monitor:") {
				monitorTag = t
				break
			}
		}
		if monitorTag == "" {
			continue
		}

		parsed := ParseMonitorTag(monitorTag)
		if parsed == nil {
			continue
		}

		// Apply filters
		if filter != nil {
			if filter.TemplateID != "" && parsed.TemplateID != filter.TemplateID {
				continue
			}
			if filter.Project != "" {
				if parsed.Scope.Type == "all" {
					continue
				}
				if parsed.Scope.Type == "project" && parsed.Scope.Project != filter.Project {
					continue
				}
				if parsed.Scope.Type == "feature" && parsed.Scope.Project != filter.Project {
					continue
				}
			}
			if filter.FeatureID != "" {
				if parsed.Scope.Type != "feature" {
					continue
				}
				if parsed.Scope.FeatureID != filter.FeatureID {
					continue
				}
			}
		}

		enabled := entry.ScheduleEnabled == nil || *entry.ScheduleEnabled
		monitors = append(monitors, MonitorInfo{
			ID:         entry.ID,
			Path:       entry.Path,
			TemplateID: parsed.TemplateID,
			Scope:      parsed.Scope,
			Enabled:    enabled,
			Schedule:   entry.Schedule,
			Title:      entry.Title,
		})
	}

	return monitors, nil
}

// Toggle toggles schedule_enabled on an existing monitor.
func (s *MonitorServiceImpl) Toggle(ctx context.Context, taskID string, enabled bool) (string, error) {
	entry, err := s.brain.Recall(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("recall monitor: %w", err)
	}

	_, err = s.brain.Update(ctx, entry.Path, types.UpdateEntryRequest{
		ScheduleEnabled: &enabled,
	})
	if err != nil {
		return "", fmt.Errorf("update monitor: %w", err)
	}

	return entry.Path, nil
}

// Delete deletes a monitor task by its ID.
func (s *MonitorServiceImpl) Delete(ctx context.Context, taskID string) (string, error) {
	entry, err := s.brain.Recall(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("recall monitor: %w", err)
	}

	if err := s.brain.Delete(ctx, entry.Path); err != nil {
		return "", fmt.Errorf("delete monitor: %w", err)
	}

	return entry.Path, nil
}
