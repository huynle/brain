package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/sdk/brain"
)

// secretAddressNote is the rule every calendar write tool states. The address
// is the only credential a calendar source has, so it is written once, kept on
// the server, and never repeated back.
const secretAddressNote = "The address is a secret: it is stored on the server, never shown again, and must not be repeated back to the user."

// RegisterCalendarTools registers the calendar source tools: a read of every
// source's status, and the admin writes that add, replace and remove runtime
// sources. Runtime addresses are secrets, so no result or error carries one.
func RegisterCalendarTools(s *Server, client *APIClient) {
	registerCalendarsList(s, client)
	registerCalendarAdd(s, client)
	registerCalendarUpdate(s, client)
	registerCalendarRemove(s, client)
}

func registerCalendarsList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "calendars_list",
		Description: `List the calendar sources the server reads.

Each source shows its origin (config, defined in server config, or runtime, added through the API), its poll interval, event count, last fetch and success, whether it is stale, and its last error. Builtin sources show only name and kind. The tool never returns a calendar address.`,
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		resp, err := sdkCall(ctx, client, func(ctx context.Context, sc *brain.Client) (*brain.CalendarsResponse, error) {
			return sc.Calendars().List(ctx)
		})
		if err != nil {
			return "", err
		}
		if len(resp.Calendars) == 0 {
			return "No calendar sources are configured.", nil
		}
		lines := []string{fmt.Sprintf("%d calendar source(s):", len(resp.Calendars))}
		for _, c := range resp.Calendars {
			lines = append(lines, "- "+describeCalendar(c))
		}
		return strings.Join(lines, "\n"), nil
	})
}

func registerCalendarAdd(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "calendar_add",
		Description: `Add an ICS calendar source, for example a Google Calendar secret iCal address, so automations can skip or require events on it.

The server checks the address with one fetch before it saves anything, so the source has events at once. Only public https addresses are accepted. Automations then name the source as calendar, skip_if_event.calendar or only_if_event.calendar.

` + secretAddressNote + `

Poll is a Go duration, at least 1m; the default is 5m.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Source name: lowercase letters, digits, '-' or '_', starting with a letter or digit, at most 64 characters"},
				"url":  {Type: "string", Description: "Secret iCal address. Stored on the server and never shown again."},
				"poll": {Type: "string", Description: "Optional poll interval, a Go duration such as 5m or 1h (minimum 1m)"},
			},
			Required: []string{"name", "url"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		name := StringArg(args, "name", "")
		address := StringArg(args, "url", "")
		if name == "" {
			return "", errors.New("name is required")
		}
		if address == "" {
			return "", errors.New("url is required")
		}
		req := brain.CreateCalendarRequest{Name: name, Url: address, Poll: optString(StringArg(args, "poll", ""))}
		status, err := sdkCall(ctx, client, func(ctx context.Context, sc *brain.Client) (*brain.CalendarStatus, error) {
			return sc.Calendars().Create(ctx, req)
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Calendar source %s added.\n- %s\n%s", status.Name, describeCalendar(*status), secretAddressNote), nil
	})
}

func registerCalendarUpdate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "calendar_update",
		Description: `Replace a runtime calendar source's address, change its poll interval, or both. Use it after the secret address has been reset in Google.

The new address is checked with one fetch before anything changes. Config sources are defined in server config and cannot be changed here.

` + secretAddressNote,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Name of the runtime calendar source"},
				"url":  {Type: "string", Description: "Replacement secret iCal address. Stored on the server and never shown again."},
				"poll": {Type: "string", Description: "New poll interval, a Go duration such as 10m (minimum 1m)"},
			},
			Required: []string{"name"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		name := StringArg(args, "name", "")
		address := StringArg(args, "url", "")
		poll := StringArg(args, "poll", "")
		if name == "" {
			return "", errors.New("name is required")
		}
		if address == "" && poll == "" {
			return "", errors.New("set url or poll")
		}
		req := brain.UpdateCalendarRequest{}
		if address != "" {
			req.Url = &address
		}
		if poll != "" {
			req.Poll = &poll
		}
		status, err := sdkCall(ctx, client, func(ctx context.Context, sc *brain.Client) (*brain.CalendarStatus, error) {
			return sc.Calendars().Update(ctx, name, req)
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Calendar source %s updated.\n- %s\n%s", status.Name, describeCalendar(*status), secretAddressNote), nil
	})
}

func registerCalendarRemove(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "calendar_remove",
		Description: `Remove a runtime calendar source and delete its stored address.

Removal is refused while automations still name the source. Set force to remove it anyway; the automations that named it then treat it as missing (skip_if_event skips nothing, only_if_event allows nothing, calendar triggers fire nothing).

` + secretAddressNote,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name":  {Type: "string", Description: "Name of the runtime calendar source"},
				"force": {Type: "boolean", Description: "Remove even when automations still reference the source (default false)"},
			},
			Required: []string{"name"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		name := StringArg(args, "name", "")
		if name == "" {
			return "", errors.New("name is required")
		}
		force := BoolArg(args, "force", false)
		resp, err := sdkCall(ctx, client, func(ctx context.Context, sc *brain.Client) (*brain.RemoveCalendarResponse, error) {
			return sc.Calendars().Delete(ctx, name, force)
		})
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("Calendar source %s removed. Its stored address is deleted.", resp.Name)}
		if len(resp.References) > 0 {
			lines = append(lines, "Automations that named it (now treated as missing):")
			for _, ref := range resp.References {
				lines = append(lines, fmt.Sprintf("- %s (%s)", ref.Title, ref.Field))
			}
		}
		return strings.Join(lines, "\n"), nil
	})
}

// describeCalendar renders one status line. It reads only the fields the
// server reports, which never include an address.
func describeCalendar(c brain.CalendarStatus) string {
	parts := []string{c.Name, string(c.Kind)}
	if c.Origin != nil {
		parts = append(parts, "origin "+string(*c.Origin))
	}
	if c.Poll != nil {
		parts = append(parts, "poll "+*c.Poll)
	}
	line := strings.Join(parts, ", ")
	if c.EventCount != nil {
		line += fmt.Sprintf(": %d event(s)", *c.EventCount)
	}
	if c.Stale != nil && *c.Stale {
		line += ", stale"
	}
	if c.LastError != nil && *c.LastError != "" {
		line += ", last error: " + *c.LastError
	}
	return line
}
