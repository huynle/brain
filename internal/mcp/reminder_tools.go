package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterReminderTools registers the reminder MCP tools.
//
// A dedicated group rather than an extension of `save`: the save tool gates
// every schedule field behind `if isTask`, so save(type:"reminder",
// remind_at:...) would report success and drop the date on the floor.
func RegisterReminderTools(s *Server, client *APIClient) {
	registerBrainReminderCreate(s, client)
	registerBrainReminderList(s, client)
	registerBrainReminderGet(s, client)
	registerBrainReminderUpdate(s, client)
	registerBrainReminderAck(s, client)
	registerBrainReminderSnooze(s, client)
	registerBrainReminderDelete(s, client)
}

// remindAtDescription is spelled out because the MCP Property struct supports
// only Type/Description/Enum/Items — there is no `format`, so writing
// format:"date-time" produces no schema output at all and the model would see
// a bare string with no hint of the required shape.
const remindAtDescription = "When to fire, RFC3339 WITH an explicit UTC offset " +
	"(e.g. \"2026-09-10T09:00:00-06:00\" or \"2026-09-10T15:00:00Z\"). " +
	"Omit for an undated reminder — something to come back to, which never fires on its own."

func reminderConfigProperties() map[string]Property {
	return map[string]Property{
		"remind_at": {Type: "string", Description: remindAtDescription},
		"timezone":  {Type: "string", Description: "IANA timezone name kept for display (e.g. America/Denver). Does not affect when it fires — remind_at's own offset does."},
		"action": {Type: "string", Enum: types.ReminderActions, Description: "What happens when it fires. " +
			"\"notify\" (default) surfaces it in the Brain app and does nothing else. " +
			"\"task\" creates a pending task so an agent works the reminder — requires a prompt."},
		"prompt":         {Type: "string", Description: "Instruction for the generated task. Required when action is \"task\"."},
		"agent":          {Type: "string", Description: "Agent override for the generated task"},
		"model":          {Type: "string", Description: "Model override for the generated task"},
		"executor":       {Type: "string", Description: "Executor override for the generated task"},
		"execution_mode": {Type: "string", Description: "Execution mode for the generated task (worktree or current_branch)"},
		"target_workdir": {Type: "string", Description: "Target workdir for the generated task"},
	}
}

func reminderConfigFromArgs(args map[string]any) types.ReminderConfig {
	return types.ReminderConfig{
		RemindAt:      StringArg(args, "remind_at", ""),
		Timezone:      StringArg(args, "timezone", ""),
		Action:        StringArg(args, "action", ""),
		Prompt:        StringArg(args, "prompt", ""),
		Agent:         StringArg(args, "agent", ""),
		Model:         StringArg(args, "model", ""),
		Executor:      StringArg(args, "executor", ""),
		ExecutionMode: StringArg(args, "execution_mode", ""),
		TargetWorkdir: StringArg(args, "target_workdir", ""),
	}
}

func registerBrainReminderCreate(s *Server, client *APIClient) {
	props := reminderConfigProperties()
	props["project"] = Property{Type: "string", Description: "Project to file the reminder under. Required unless global is true."}
	props["global"] = Property{Type: "boolean", Description: "File as a global reminder instead of under a project"}
	props["feature_id"] = Property{Type: "string", Description: "Optional feature to scope the reminder to"}
	props["title"] = Property{Type: "string", Description: "What to be reminded about"}
	props["content"] = Property{Type: "string", Description: "Optional longer body"}
	props["tags"] = Property{Type: "array", Items: &Property{Type: "string"}, Description: "Optional tags"}

	s.RegisterTool(Tool{
		Name: "reminder_create",
		Description: "Create a reminder — something to come back to, optionally at a specific date and time.\n\n" +
			"With remind_at it fires at that moment: either notifying in the Brain app (default) or " +
			"creating a task for an agent to work. Without remind_at it is simply a durable note to " +
			"revisit, which never fires on its own.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"title"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		title := strings.TrimSpace(StringArg(args, "title", ""))
		if title == "" {
			return "", fmt.Errorf("provide a 'title'")
		}
		project := strings.TrimSpace(StringArg(args, "project", ""))
		global := BoolArg(args, "global", false)
		// Deliberately NOT falling back to ResolveProject: over the in-process
		// HTTP transport the ambient context is derived from the API host's
		// own working directory, so an omitted project would silently file the
		// reminder under whatever project that host resembles.
		if project == "" && !global {
			return "", fmt.Errorf("provide a 'project' (or set global: true) — " +
				"a reminder filed under the wrong project is one nobody will see")
		}

		req := types.CreateReminderRequest{
			Project:   project,
			FeatureID: StringArg(args, "feature_id", ""),
			Title:     title,
			Content:   StringArg(args, "content", ""),
			Tags:      StringSliceArg(args, "tags"),
			Config:    reminderConfigFromArgs(args),
		}
		if global {
			req.Global = &global
		}

		var out types.ReminderSummary
		if err := client.Request(ctx, http.MethodPost, "/reminders", req, nil, &out); err != nil {
			return "", err
		}
		return formatReminderSummary("Reminder created", &out), nil
	})
}

func registerBrainReminderList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "reminder_list",
		Description: "List reminders. Use state=\"fired\" to see what is waiting to be acknowledged.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project": {Type: "string", Description: "Filter to one project"},
			"state": {Type: "string", Enum: []string{
				types.ReminderStateArmed, types.ReminderStateUndated,
				types.ReminderStateFired, types.ReminderStateDone,
				types.ReminderStatePaused,
			}, Description: "Filter by lifecycle state: armed (dated, waiting), undated, fired (waiting to be acknowledged), done, paused"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		q := map[string]string{}
		if v := StringArg(args, "project", ""); v != "" {
			q["project"] = v
		}
		if v := StringArg(args, "state", ""); v != "" {
			q["state"] = v
		}
		var out types.ReminderListResponse
		if err := client.Request(ctx, http.MethodGet, "/reminders", nil, q, &out); err != nil {
			return "", err
		}
		if len(out.Reminders) == 0 {
			return "No reminders found.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d reminder(s):\n\n", out.Count)
		for i := range out.Reminders {
			r := out.Reminders[i]
			fmt.Fprintf(&b, "- **%s** (`%s`) — %s", r.Title, r.ReminderID, r.State)
			if r.RemindAt != "" {
				fmt.Fprintf(&b, " · %s", r.RemindAt)
			}
			if r.Action != "" && r.Action != types.ReminderActionNotify {
				fmt.Fprintf(&b, " · action: %s", r.Action)
			}
			if r.Project != "" {
				fmt.Fprintf(&b, " · %s", r.Project)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	})
}

func registerBrainReminderGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "reminder_get",
		Description: "Get one reminder by its reminder id.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"reminder_id": {Type: "string", Description: "The reminder's id"},
		}, Required: []string{"reminder_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "reminder_id", ""))
		if id == "" {
			return "", fmt.Errorf("provide a 'reminder_id'")
		}
		var out types.ReminderSummary
		if err := client.Request(ctx, http.MethodGet, "/reminders/"+url.PathEscape(id), nil, nil, &out); err != nil {
			return "", err
		}
		return formatReminderSummary("Reminder", &out), nil
	})
}

func registerBrainReminderUpdate(s *Server, client *APIClient) {
	props := reminderConfigProperties()
	props["reminder_id"] = Property{Type: "string", Description: "The reminder's id"}
	props["title"] = Property{Type: "string", Description: "New title"}
	props["content"] = Property{Type: "string", Description: "New body"}
	props["status"] = Property{Type: "string", Description: "New entry status (active, pending, completed, blocked)"}

	s.RegisterTool(Tool{
		Name: "reminder_update",
		Description: "Update a reminder. Passing remind_at as an empty string CLEARS the date, " +
			"turning a scheduled reminder back into an undated one.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"reminder_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "reminder_id", ""))
		if id == "" {
			return "", fmt.Errorf("provide a 'reminder_id'")
		}
		// Presence-based, not emptiness-based: an emptiness check cannot
		// distinguish "clear the date" from "leave it alone", so clearing
		// would be inexpressible.
		req := types.UpdateReminderRequest{
			Title:         clearableStringArg(args, "title"),
			Content:       clearableStringArg(args, "content"),
			Status:        clearableStringArg(args, "status"),
			RemindAt:      clearableStringArg(args, "remind_at"),
			Timezone:      clearableStringArg(args, "timezone"),
			Action:        clearableStringArg(args, "action"),
			Prompt:        clearableStringArg(args, "prompt"),
			Agent:         clearableStringArg(args, "agent"),
			Model:         clearableStringArg(args, "model"),
			Executor:      clearableStringArg(args, "executor"),
			ExecutionMode: clearableStringArg(args, "execution_mode"),
			TargetWorkdir: clearableStringArg(args, "target_workdir"),
		}
		var out types.ReminderSummary
		if err := client.Request(ctx, http.MethodPatch, "/reminders/"+url.PathEscape(id), req, nil, &out); err != nil {
			return "", err
		}
		return formatReminderSummary("Reminder updated", &out), nil
	})
}

func registerBrainReminderAck(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "reminder_ack",
		Description: "Acknowledge a fired reminder, clearing it from the pending list.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"reminder_id": {Type: "string", Description: "The reminder's id"},
		}, Required: []string{"reminder_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "reminder_id", ""))
		if id == "" {
			return "", fmt.Errorf("provide a 'reminder_id'")
		}
		var out types.ReminderSummary
		if err := client.Request(ctx, http.MethodPost, "/reminders/"+url.PathEscape(id)+"/ack", nil, nil, &out); err != nil {
			return "", err
		}
		return formatReminderSummary("Reminder acknowledged", &out), nil
	})
}

func registerBrainReminderSnooze(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "reminder_snooze",
		Description: "Re-arm a reminder for a new time. This is the way to make a fired reminder " +
			"fire again — simply setting its status back to active will not, because each firing " +
			"is claimed once per remind_at.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"reminder_id": {Type: "string", Description: "The reminder's id"},
			"remind_at":   {Type: "string", Description: remindAtDescription},
		}, Required: []string{"reminder_id", "remind_at"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "reminder_id", ""))
		at := strings.TrimSpace(StringArg(args, "remind_at", ""))
		if id == "" || at == "" {
			return "", fmt.Errorf("provide a 'reminder_id' and a 'remind_at'")
		}
		body := map[string]string{"remind_at": at}
		var out types.ReminderSummary
		if err := client.Request(ctx, http.MethodPost, "/reminders/"+url.PathEscape(id)+"/snooze", body, nil, &out); err != nil {
			return "", err
		}
		return formatReminderSummary("Reminder snoozed", &out), nil
	})
}

func registerBrainReminderDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "reminder_delete",
		Description: "Delete a reminder permanently.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"reminder_id": {Type: "string", Description: "The reminder's id"},
		}, Required: []string{"reminder_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "reminder_id", ""))
		if id == "" {
			return "", fmt.Errorf("provide a 'reminder_id'")
		}
		if err := client.Request(ctx, http.MethodDelete, "/reminders/"+url.PathEscape(id), nil, nil, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("Reminder `%s` deleted.", id), nil
	})
}

func formatReminderSummary(heading string, r *types.ReminderSummary) string {
	if r == nil {
		return heading
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: **%s**\n", heading, r.Title)
	fmt.Fprintf(&b, "- reminder_id: `%s`\n", r.ReminderID)
	fmt.Fprintf(&b, "- state: %s\n", r.State)
	if r.RemindAt != "" {
		fmt.Fprintf(&b, "- remind_at: %s\n", r.RemindAt)
	} else {
		b.WriteString("- remind_at: (undated — will not fire on its own)\n")
	}
	if r.Timezone != "" {
		fmt.Fprintf(&b, "- timezone: %s\n", r.Timezone)
	}
	fmt.Fprintf(&b, "- action: %s\n", r.Action)
	if r.Project != "" {
		fmt.Fprintf(&b, "- project: %s\n", r.Project)
	}
	if r.FiredAt != "" {
		fmt.Fprintf(&b, "- fired_at: %s\n", r.FiredAt)
	}
	if r.GeneratedTaskID != "" {
		fmt.Fprintf(&b, "- generated task: `%s`\n", r.GeneratedTaskID)
	}
	return b.String()
}
