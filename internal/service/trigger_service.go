package service

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Interfaces
// =============================================================================

// TriggerTaskStore abstracts the storage operations needed by TriggerService.
// Implementations should query for task entries that have trigger configurations
// and update task status when triggers fire.
type TriggerTaskStore interface {
	// ListTriggeredTasks returns all task entries that have a trigger configured.
	ListTriggeredTasks(ctx context.Context) ([]types.BrainEntry, error)

	// ActivateTask sets a task to pending with the given metadata fields.
	ActivateTask(ctx context.Context, path string, fields map[string]interface{}) error

	// CountInProgressByTrigger counts tasks that are currently in_progress
	// and have a trigger matching the given event pattern within a project.
	CountInProgressByTrigger(ctx context.Context, triggerEvent, projectID string) (int, error)
}

// =============================================================================
// TriggerResult
// =============================================================================

// TriggerResult holds the evaluation outcome for a single task trigger.
type TriggerResult struct {
	// TaskPath is the brain path of the matched task.
	TaskPath string
	// TaskID is the 8-char short ID.
	TaskID string
	// Matched is true if the event matched the trigger conditions.
	Matched bool
	// Reason explains why the trigger was skipped (cooldown, max_concurrent, etc.).
	Reason string
	// InterpolatedPrompt is the direct_prompt with event data interpolated.
	InterpolatedPrompt string
	// EventID is the ID of the event that triggered this evaluation.
	EventID string
}

// =============================================================================
// TriggerService
// =============================================================================

// TriggerService evaluates incoming events against task entries with trigger
// frontmatter, handling cooldown, max_concurrent, template interpolation,
// and task activation.
type TriggerService struct {
	store TriggerTaskStore

	// cooldowns tracks the last trigger time per task path for cooldown enforcement.
	mu        sync.RWMutex
	cooldowns map[string]time.Time
}

// NewTriggerService creates a new TriggerService.
func NewTriggerService(store TriggerTaskStore) *TriggerService {
	return &TriggerService{
		store:     store,
		cooldowns: make(map[string]time.Time),
	}
}

// eligibleStatuses defines which task statuses are eligible for event triggering.
// - "active": tasks waiting for a trigger
// - "completed": recurring tasks that can be re-triggered
// - "blocked": tasks that may become unblocked by an event
var eligibleStatuses = map[string]bool{
	"active":    true,
	"completed": true,
	"blocked":   true,
}

// =============================================================================
// Evaluate
// =============================================================================

// Evaluate checks an event against all configured triggers and returns results.
// It does NOT modify any task state; use Activate for that.
func (s *TriggerService) Evaluate(ctx context.Context, evt types.Event) ([]TriggerResult, error) {
	entries, err := s.store.ListTriggeredTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list triggered tasks: %w", err)
	}

	var results []TriggerResult

	for _, entry := range entries {
		if entry.Trigger == nil || entry.Trigger.Event == "" {
			continue
		}

		// Skip tasks in non-eligible status.
		if !eligibleStatuses[entry.Status] {
			continue
		}

		// Prevent self-triggering: don't let an event about a task trigger itself.
		if evt.TaskID != "" && (evt.TaskID == entry.ID || evt.TaskPath == entry.Path) {
			continue
		}

		// Step 1: Match event type pattern.
		if !types.MatchEventPattern(entry.Trigger.Event, evt.Type) {
			continue
		}

		// Step 2: Match filters against event fields.
		if !matchTriggerFilters(entry.Trigger.Filter, evt) {
			continue
		}

		// Step 3: Check cooldown.
		if skip, reason := s.checkCooldown(entry.Path, entry.Trigger.Cooldown); skip {
			// Don't include skipped triggers in results (they're silently suppressed).
			_ = reason
			continue
		}

		// Step 4: Check max_concurrent.
		if entry.Trigger.MaxConcurrent > 0 {
			count, err := s.store.CountInProgressByTrigger(ctx, entry.Trigger.Event, entry.ProjectID)
			if err != nil {
				// On error, skip this trigger rather than failing everything.
				continue
			}
			if count >= entry.Trigger.MaxConcurrent {
				continue
			}
		}

		// Step 5: Template-interpolate direct_prompt.
		prompt := interpolatePrompt(entry.DirectPrompt, evt)

		results = append(results, TriggerResult{
			TaskPath:           entry.Path,
			TaskID:             entry.ID,
			Matched:            true,
			InterpolatedPrompt: prompt,
			EventID:            evt.ID,
		})
	}

	return results, nil
}

// =============================================================================
// Activate
// =============================================================================

// Activate sets matched tasks to "pending" so the runner picks them up.
// Returns the count of activated tasks.
func (s *TriggerService) Activate(ctx context.Context, results []TriggerResult) (int, error) {
	activated := 0
	for _, r := range results {
		if !r.Matched {
			continue
		}

		fields := map[string]interface{}{
			"status":             "pending",
			"triggered_by_event": r.EventID,
			"triggered_at":       types.TimeNowUTC().Format(time.RFC3339),
		}

		// If we have an interpolated prompt, include it.
		if r.InterpolatedPrompt != "" {
			fields["direct_prompt"] = r.InterpolatedPrompt
		}

		if err := s.store.ActivateTask(ctx, r.TaskPath, fields); err != nil {
			return activated, fmt.Errorf("activate task %s: %w", r.TaskPath, err)
		}

		// Record the trigger time for cooldown tracking.
		s.mu.Lock()
		s.cooldowns[r.TaskPath] = types.TimeNowUTC()
		s.mu.Unlock()

		activated++
	}
	return activated, nil
}

// =============================================================================
// Filter Matching
// =============================================================================

// matchTriggerFilters checks whether an event matches all filter key-value pairs.
// Filter keys map to event struct fields: project_id, feature_id, task_id, source,
// from_status, to_status. Unknown keys are checked against event Metadata.
func matchTriggerFilters(filters map[string]string, evt types.Event) bool {
	for key, expected := range filters {
		actual := getEventField(evt, key)
		if actual != expected {
			return false
		}
	}
	return true
}

// getEventField returns the value of an event field by key name.
// Falls back to event Metadata for unknown keys.
func getEventField(evt types.Event, key string) string {
	switch key {
	case "project_id":
		return evt.ProjectID
	case "feature_id":
		return evt.FeatureID
	case "task_id":
		return evt.TaskID
	case "source":
		return evt.Source
	case "runner_id":
		return evt.RunnerID
	case "from_status":
		return evt.FromStatus
	case "to_status":
		return evt.ToStatus
	case "type":
		return evt.Type
	default:
		if evt.Metadata != nil {
			return evt.Metadata[key]
		}
		return ""
	}
}

// =============================================================================
// Cooldown
// =============================================================================

// checkCooldown returns true if the task is still in its cooldown period.
func (s *TriggerService) checkCooldown(taskPath, cooldownStr string) (skip bool, reason string) {
	if cooldownStr == "" {
		return false, ""
	}

	duration, err := time.ParseDuration(cooldownStr)
	if err != nil {
		// Invalid cooldown format: treat as no cooldown.
		return false, ""
	}

	s.mu.RLock()
	lastTrigger, exists := s.cooldowns[taskPath]
	s.mu.RUnlock()

	if !exists {
		return false, ""
	}

	elapsed := types.TimeNowUTC().Sub(lastTrigger)
	if elapsed < duration {
		remaining := duration - elapsed
		return true, fmt.Sprintf("cooldown active: %v remaining", remaining.Round(time.Second))
	}

	return false, ""
}

// =============================================================================
// Template Interpolation
// =============================================================================

// interpolatePrompt applies Go template interpolation to a direct_prompt string
// using event data. If the prompt contains no template syntax or if templating
// fails, the original prompt is returned unchanged.
func interpolatePrompt(prompt string, evt types.Event) string {
	if prompt == "" {
		return ""
	}

	tmpl, err := template.New("trigger").Option("missingkey=error").Parse(prompt)
	if err != nil {
		// Template parse error: return original.
		return prompt
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, evt); err != nil {
		// Template execution error (missing field, etc.): return original.
		return prompt
	}

	return buf.String()
}
