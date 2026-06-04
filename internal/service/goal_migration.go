package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// goalV1PlanTag is the V1 tag that marks a goal plan entry.
const goalV1PlanTag = "goal:plan"

// goalSessionModeRe extracts the goal_session_mode metadata value from a plan
// body. It mirrors the regex used by the legacy goal command.
var goalSessionModeRe = regexp.MustCompile(`(?m)^\s*-?\s*goal_session_mode\s*:\s*([A-Za-z0-9_-]+)\s*$`)

// Placeholder lines emitted by the legacy buildPlanContent when a section was
// left unconfigured. These are skipped during conversion.
const (
	goalCriteriaPlaceholder   = "Define measurable success criteria."
	goalValidationPlaceholder = "_No validation commands configured._"
)

// LegacyGoalToInput converts a legacy V1 goal (a goal:plan entry plus its
// optional goal:reconciler task) into a GoalInput for BuildGoalAutomation.
// reconciler may be nil if the plan has no paired reconciler task.
//
// Limitation: the legacy goal carried an executor (plan.Executor /
// reconciler.Executor / the body "executor:" metadata). Neither GoalInput nor
// types.AutomationAction has an Executor field, so the executor cannot be
// carried forward through this converter. We intentionally do NOT add new
// fields in this phase; callers that need executor fidelity must handle it out
// of band.
func LegacyGoalToInput(plan types.BrainEntry, reconciler *types.BrainEntry) (GoalInput, error) {
	if !isGoalPlan(plan) {
		return GoalInput{}, fmt.Errorf("legacy goal: entry %q is not a goal plan (type=%q, tags=%v)", plan.Title, plan.Type, plan.Tags)
	}
	title := strings.TrimSpace(plan.Title)
	if title == "" {
		return GoalInput{}, fmt.Errorf("legacy goal: plan has empty title")
	}

	body := plan.Content
	slug := goalIDFromPlan(plan)

	// Config.
	cfg := types.GoalConfig{
		ID:            slug,
		Criteria:      parseGoalCriteria(body, plan.Content),
		Validation:    parseGoalValidation(body),
		Workdir:       firstNonEmpty(plan.TargetWorkdir, parseGoalMetadataLine(body, "target_workdir")),
		TriggerSource: parseGoalTriggerSource(body),
	}

	// Action.
	action := types.AutomationAction{
		Type:        "prompt",
		Agent:       firstNonEmpty(reconcilerField(reconciler, func(r *types.BrainEntry) string { return r.Agent }), plan.Agent, parseGoalMetadataLine(body, "agent")),
		Model:       firstNonEmpty(reconcilerField(reconciler, func(r *types.BrainEntry) string { return r.Model }), plan.Model, parseGoalMetadataLine(body, "model")),
		SessionMode: parseGoalSessionMode(body),
	}
	action.DirectPrompt = goalReconcilePrompt(reconciler, plan, cfg)
	if reconciler != nil && reconciler.CompleteOnIdle != nil {
		action.CompleteOnIdle = reconciler.CompleteOnIdle
	}

	return GoalInput{
		Project:   plan.ProjectID,
		FeatureID: firstNonEmpty(plan.FeatureID, reconcilerField(reconciler, func(r *types.BrainEntry) string { return r.FeatureID }), parseGoalMetadataLine(body, "feature_id")),
		Title:     title,
		// Content intentionally left empty: BuildGoalAutomation defaults the
		// entry body to the goal criteria.
		Content: "",
		Config:  cfg,
		Action:  action,
	}, nil
}

// isGoalPlan reports whether the entry is a legacy goal plan, identified by
// either type "plan" or the goal:plan tag.
func isGoalPlan(plan types.BrainEntry) bool {
	if plan.Type == "plan" {
		return true
	}
	for _, tag := range plan.Tags {
		if tag == goalV1PlanTag {
			return true
		}
	}
	return false
}

// goalIDFromPlan derives the stable goal slug from the plan. It prefers the
// GeneratedKey ("goal:<slug>:plan"); if that is empty or unparseable it falls
// back to slugifying the plan title.
func goalIDFromPlan(plan types.BrainEntry) string {
	if slug := slugFromGeneratedKey(plan.GeneratedKey); slug != "" {
		return slug
	}
	return goalV1Slug(plan.Title)
}

// slugFromGeneratedKey extracts the slug from a "goal:<slug>:plan" key. Returns
// empty when the key does not match that shape.
func slugFromGeneratedKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	const prefix = "goal:"
	const suffix = ":plan"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return ""
	}
	slug := key[len(prefix) : len(key)-len(suffix)]
	return strings.TrimSpace(slug)
}

// goalV1Slug normalizes a string into a goal slug: lowercase, trimmed, runs of
// non-alphanumeric characters collapsed to "-", leading/trailing "-" trimmed.
// Empty results become "goal". This replicates the legacy goal slug algorithm.
var goalV1SlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func goalV1Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = goalV1SlugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "goal"
	}
	return s
}

// parseGoalCriteria joins the bullets under "## Acceptance Criteria",
// stripping the leading "- " and skipping the placeholder line. When no real
// bullets are found it falls back to the trimmed plan content.
func parseGoalCriteria(body, fallback string) string {
	bullets := parseGoalBullets(body, "## Acceptance Criteria")
	var out []string
	for _, b := range bullets {
		if b == goalCriteriaPlaceholder {
			continue
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return strings.TrimSpace(fallback)
	}
	return strings.Join(out, "\n")
}

// parseGoalValidation joins the bullets under "## Validation Commands",
// stripping the leading "- " and surrounding backticks, and skipping the
// placeholder line. Returns empty when no real commands are present.
func parseGoalValidation(body string) string {
	bullets := parseGoalBullets(body, "## Validation Commands")
	var out []string
	for _, b := range bullets {
		if b == goalValidationPlaceholder {
			continue
		}
		cmd := strings.Trim(b, "`")
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		out = append(out, cmd)
	}
	return strings.Join(out, "\n")
}

// parseGoalBullets returns the bullet contents under the given "## Header"
// section. A bullet is a line beginning with "- "; the leading marker is
// stripped. Parsing stops at the next "## " header or end of body. Non-bullet
// lines (e.g. the validation placeholder) are also returned trimmed so callers
// can recognize placeholders.
func parseGoalBullets(body, header string) []string {
	lines := strings.Split(body, "\n")
	var out []string
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if inSection {
				break
			}
			if trimmed == header {
				inSection = true
			}
			continue
		}
		if !inSection {
			continue
		}
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		// Non-bullet content (e.g. a placeholder paragraph). Surface it so
		// placeholder detection can skip it.
		out = append(out, trimmed)
	}
	return out
}

// parseGoalMetadataLine returns the value of a "- key: value" line from the
// "## Execution Metadata" section (or anywhere in the body). Returns empty when
// the key is absent or has an empty value.
func parseGoalMetadataLine(body, key string) string {
	re := regexp.MustCompile(`(?m)^\s*-?\s*` + regexp.QuoteMeta(key) + `\s*:\s*(.*?)\s*$`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// parseGoalSessionMode extracts the goal_session_mode value from the body,
// defaulting to "continue" when absent.
func parseGoalSessionMode(body string) string {
	m := goalSessionModeRe.FindStringSubmatch(body)
	if m == nil {
		return "continue"
	}
	mode := strings.TrimSpace(m[1])
	if mode == "" {
		return "continue"
	}
	return mode
}

// parseGoalTriggerSource maps the body "trigger: task.completed" metadata to a
// goal trigger source. When absent it returns empty so BuildGoalAutomation
// applies its default ("both").
func parseGoalTriggerSource(body string) string {
	if parseGoalMetadataLine(body, "trigger") == "task.completed" {
		return types.GoalTriggerSourceTask
	}
	return ""
}

// goalReconcilePrompt selects the reconcile prompt, preferring the reconciler
// task's DirectPrompt, then the plan content, then a prompt derived from the
// goal criteria.
func goalReconcilePrompt(reconciler *types.BrainEntry, plan types.BrainEntry, cfg types.GoalConfig) string {
	if reconciler != nil {
		if p := strings.TrimSpace(reconciler.DirectPrompt); p != "" {
			return reconciler.DirectPrompt
		}
	}
	if p := strings.TrimSpace(plan.Content); p != "" {
		return plan.Content
	}
	if c := strings.TrimSpace(cfg.Criteria); c != "" {
		return "Reconcile this goal until the following criteria are met:\n" + cfg.Criteria
	}
	return "Reconcile this goal until its acceptance criteria are met."
}

// reconcilerField safely extracts a field from a possibly-nil reconciler.
func reconcilerField(reconciler *types.BrainEntry, get func(*types.BrainEntry) string) string {
	if reconciler == nil {
		return ""
	}
	return get(reconciler)
}

// firstNonEmpty returns the first trimmed-non-empty value, or empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
