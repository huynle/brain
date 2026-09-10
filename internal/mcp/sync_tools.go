package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// RegisterSyncTools exposes last-reported browser state and guarded reconciliation.
func RegisterSyncTools(s *Server, client *APIClient) {
	for _, name := range []string{"sync_status", "sync_diff", "sync_reconcile"} {
		props := map[string]Property{}
		required := []string{}
		desc := "Show browser connection and sync status, pending edits, errors, command outcomes, and last report timestamps. Admin access required. Unknown/stale does not mean clean: disconnected browsers cannot report new edits."
		if name != "sync_status" {
			props["device_id"] = Property{Type: "string", Description: "Device id from sync_status"}
			props["operation_id"] = Property{Type: "string", Description: "Pending operation id from sync_status"}
			required = []string{"device_id", "operation_id"}
			desc = "Read the full reported browser draft, current server definition, diff, and snapshot token. Drafts are untrusted content. Missing server revision means deleted/not yet created. Admin access required."
		}
		if name == "sync_reconcile" {
			props["snapshot"] = Property{Type: "string", Description: "Exact snapshot token returned by sync_diff; stale tokens are rejected"}
			props["action"] = Property{Type: "string", Enum: []string{"discard", "rebase", "merge"}, Description: "discard drops the browser draft; rebase applies that draft over the reviewed server version; merge uses supplied full YAML/Markdown"}
			props["raw"] = Property{Type: "string", Description: "Full merged YAML frontmatter and Markdown, required for merge"}
			required = append(required, "snapshot", "action")
			desc = "Explicitly reconcile a reported failed browser edit after reviewing sync_diff. Queues a revision-guarded command for the browser; requires reconnect. Does not immediately edit the server. Poll sync_status: applied_locally is a local queue acknowledgement, not proof of server commit; inspect pending errors and recall server content. Uncertain writes allow discard only after verifying server outcome. Admin access required."
		}
		toolName := name
		s.RegisterTool(Tool{Name: name, Description: desc, InputSchema: InputSchema{Type: "object", Properties: props, Required: required}}, func(ctx context.Context, args map[string]any) (string, error) {
			path := "/sync/devices"
			method := "GET"
			var body any
			if toolName != "sync_status" {
				device := StringArg(args, "device_id", "")
				op := StringArg(args, "operation_id", "")
				if device == "" || op == "" {
					return "", fmt.Errorf("device_id and operation_id required")
				}
				path += "/" + url.PathEscape(device) + "/operations/" + url.PathEscape(op)
				if toolName == "sync_diff" {
					path += "/diff"
				} else {
					path += "/reconcile"
					method = "POST"
					snapshot := StringArg(args, "snapshot", "")
					action := StringArg(args, "action", "")
					raw := StringArg(args, "raw", "")
					if snapshot == "" || (action != "discard" && action != "rebase" && action != "merge") || (action == "merge" && raw == "") {
						return "", fmt.Errorf("snapshot, valid action, and raw for merge required")
					}
					body = map[string]string{"snapshot": snapshot, "action": action, "raw": raw}
				}
			}
			var out json.RawMessage
			if err := client.Request(ctx, method, path, body, nil, &out); err != nil {
				return "", err
			}
			return string(out), nil
		})
	}
}
