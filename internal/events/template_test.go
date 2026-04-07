package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TemplateContext population from event payload
// =============================================================================

func TestNewTemplateContext_PopulatesFromEventPayload(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:      TaskCompleted,
		ProjectID: "my-project",
		Timestamp: now,
		Payload: map[string]any{
			"feature_id":          "feat-auth",
			"id":                  "abc12def",
			"path":                "projects/my-project/task/abc12def.md",
			"workdir":             "/home/user/projects/brain-api",
			"merge_target_branch": "main",
			"previous_output":     "Tests passed",
			"runner_id":           "runner-42",
		},
	}

	ctx := NewTemplateContext(event)

	assert.Equal(t, "my-project", ctx.Project)
	assert.Equal(t, "feat-auth", ctx.FeatureID)
	assert.Equal(t, "abc12def", ctx.TaskID)
	assert.Equal(t, "projects/my-project/task/abc12def.md", ctx.TaskPath)
	assert.Equal(t, "/home/user/projects/brain-api", ctx.Workdir)
	assert.Equal(t, "main", ctx.MergeTargetBranch)
	assert.Equal(t, "Tests passed", ctx.PreviousTaskOutput)
	assert.Equal(t, "runner-42", ctx.RunnerID)
	assert.Equal(t, string(TaskCompleted), ctx.Event)
	assert.NotEmpty(t, ctx.Date)
	assert.NotEmpty(t, ctx.DayOfWeek)
}

func TestNewTemplateContext_PopulatesPayloadMap(t *testing.T) {
	event := Event{
		Type:      EntryCreated,
		ProjectID: "proj",
		Payload: map[string]any{
			"custom_key": "custom_value",
			"number":     42,
		},
	}

	ctx := NewTemplateContext(event)

	assert.Equal(t, "custom_value", ctx.Payload["custom_key"])
	assert.Equal(t, 42, ctx.Payload["number"])
}

func TestNewTemplateContext_MissingFieldsAreEmptyStrings(t *testing.T) {
	event := Event{
		Type:      TaskCompleted,
		ProjectID: "proj",
		Payload:   map[string]any{},
	}

	ctx := NewTemplateContext(event)

	assert.Equal(t, "proj", ctx.Project)
	assert.Equal(t, "", ctx.FeatureID)
	assert.Equal(t, "", ctx.TaskID)
	assert.Equal(t, "", ctx.TaskPath)
	assert.Equal(t, "", ctx.Workdir)
	assert.Equal(t, "", ctx.MergeTargetBranch)
	assert.Equal(t, "", ctx.PreviousTaskOutput)
	assert.Equal(t, "", ctx.RunnerID)
}

func TestNewTemplateContext_ScheduleTimeAndRunNumber(t *testing.T) {
	schedTime := time.Now().Format(time.RFC3339)
	event := Event{
		Type:      ScheduleFired,
		ProjectID: "proj",
		Payload: map[string]any{
			"schedule_time": schedTime,
			"run_number":    5,
		},
	}

	ctx := NewTemplateContext(event)

	assert.Equal(t, schedTime, ctx.ScheduleTime)
	assert.Equal(t, 5, ctx.RunNumber)
}

func TestNewTemplateContext_NilPayloadDoesNotPanic(t *testing.T) {
	event := Event{
		Type:      TaskCompleted,
		ProjectID: "proj",
		Payload:   nil,
	}

	// Should not panic
	ctx := NewTemplateContext(event)
	assert.Equal(t, "proj", ctx.Project)
	assert.NotNil(t, ctx.Payload)
}

// =============================================================================
// ResolveTemplate — direct_prompt and command resolution
// =============================================================================

func TestResolveTemplate_BasicVariableSubstitution(t *testing.T) {
	ctx := TemplateContext{
		Project:   "brain-api",
		TaskID:    "abc12def",
		FeatureID: "auth",
	}

	result, err := ResolveTemplate("Deploy {{.Project}} task {{.TaskID}} for feature {{.FeatureID}}", ctx)

	require.NoError(t, err)
	assert.Equal(t, "Deploy brain-api task abc12def for feature auth", result)
}

func TestResolveTemplate_EmptyTemplate(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	result, err := ResolveTemplate("", ctx)

	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestResolveTemplate_NoVariables(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	result, err := ResolveTemplate("plain text with no variables", ctx)

	require.NoError(t, err)
	assert.Equal(t, "plain text with no variables", result)
}

func TestResolveTemplate_PayloadMapAccess(t *testing.T) {
	ctx := TemplateContext{
		Payload: map[string]any{
			"custom": "hello",
		},
	}

	result, err := ResolveTemplate("Value: {{index .Payload \"custom\"}}", ctx)

	require.NoError(t, err)
	assert.Equal(t, "Value: hello", result)
}

func TestResolveTemplate_MissingVariableRendersEmpty(t *testing.T) {
	ctx := TemplateContext{
		Project: "proj",
		// TaskID is empty
	}

	result, err := ResolveTemplate("Project: {{.Project}}, Task: {{.TaskID}}", ctx)

	require.NoError(t, err)
	assert.Equal(t, "Project: proj, Task: ", result)
}

func TestResolveTemplate_InvalidSyntaxReturnsError(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	_, err := ResolveTemplate("Bad template {{.Unclosed", ctx)

	assert.Error(t, err)
}

func TestResolveTemplate_UndefinedFieldReturnsError(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	// Accessing a field that doesn't exist on the struct should error
	_, err := ResolveTemplate("{{.NonExistentField}}", ctx)

	assert.Error(t, err)
}

func TestResolveTemplate_AllFieldsAccessible(t *testing.T) {
	ctx := TemplateContext{
		Project:            "proj",
		FeatureID:          "feat",
		TaskID:             "task1",
		TaskPath:           "projects/proj/task/task1.md",
		Workdir:            "/tmp",
		MergeTargetBranch:  "main",
		PreviousTaskOutput: "output",
		Event:              "task.completed",
		ScheduleTime:       "2026-01-01T00:00:00Z",
		RunNumber:          3,
		RunnerID:           "runner-1",
		Date:               "2026-01-01",
		DayOfWeek:          "Thursday",
	}

	tmpl := "{{.Project}} {{.FeatureID}} {{.TaskID}} {{.TaskPath}} {{.Workdir}} " +
		"{{.MergeTargetBranch}} {{.PreviousTaskOutput}} {{.Event}} " +
		"{{.ScheduleTime}} {{.RunNumber}} {{.RunnerID}} {{.Date}} {{.DayOfWeek}}"

	result, err := ResolveTemplate(tmpl, ctx)

	require.NoError(t, err)
	expected := "proj feat task1 projects/proj/task/task1.md /tmp main output task.completed 2026-01-01T00:00:00Z 3 runner-1 2026-01-01 Thursday"
	assert.Equal(t, expected, result)
}

// =============================================================================
// ResolveActionTemplates — resolve both direct_prompt and command
// =============================================================================

func TestResolveActionTemplates_ResolvesDirectPrompt(t *testing.T) {
	prompt := "Review task {{.TaskID}} in {{.Project}}"
	command := ""

	ctx := TemplateContext{
		Project: "brain-api",
		TaskID:  "abc12def",
	}

	resolvedPrompt, resolvedCmd, err := ResolveActionTemplates(prompt, command, ctx)

	require.NoError(t, err)
	assert.Equal(t, "Review task abc12def in brain-api", resolvedPrompt)
	assert.Equal(t, "", resolvedCmd)
}

func TestResolveActionTemplates_ResolvesCommand(t *testing.T) {
	prompt := ""
	command := "cd {{.Workdir}} && make test"

	ctx := TemplateContext{
		Workdir: "/home/user/brain-api",
	}

	resolvedPrompt, resolvedCmd, err := ResolveActionTemplates(prompt, command, ctx)

	require.NoError(t, err)
	assert.Equal(t, "", resolvedPrompt)
	assert.Equal(t, "cd /home/user/brain-api && make test", resolvedCmd)
}

func TestResolveActionTemplates_ResolvesBoth(t *testing.T) {
	prompt := "Run tests for {{.Project}}"
	command := "cd {{.Workdir}} && go test ./..."

	ctx := TemplateContext{
		Project: "brain-api",
		Workdir: "/home/user/brain-api",
	}

	resolvedPrompt, resolvedCmd, err := ResolveActionTemplates(prompt, command, ctx)

	require.NoError(t, err)
	assert.Equal(t, "Run tests for brain-api", resolvedPrompt)
	assert.Equal(t, "cd /home/user/brain-api && go test ./...", resolvedCmd)
}

func TestResolveActionTemplates_InvalidPromptReturnsError(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	_, _, err := ResolveActionTemplates("{{.Broken", "", ctx)

	assert.Error(t, err)
}

func TestResolveActionTemplates_InvalidCommandReturnsError(t *testing.T) {
	ctx := TemplateContext{Project: "proj"}

	_, _, err := ResolveActionTemplates("valid prompt", "{{.Broken", ctx)

	assert.Error(t, err)
}
