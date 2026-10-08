package mcp

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/sdk/brain"
)

// RegisterSyncTools exposes last-reported browser state and guarded reconciliation.
func RegisterSyncTools(s *Server, client *APIClient) {
	for _, name := range []string{"sync_status", "sync_diff", "sync_reconcile"} {
		props := map[string]Property{}
		required := []string{}
		desc := "Show browser connection and sync status, pending edits, errors, command outcomes, and last report timestamps. cache_mode=recent means only the reported cached_entries working set is stored; ready does not imply full-library coverage and cursor is a local cache generation. Admin access required. Unknown/stale does not mean clean: disconnected browsers cannot report new edits."
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
			call := func(ctx context.Context, sc *brain.Client) error {
				_, err := sc.Sync().Devices(ctx)
				return err
			}
			if toolName != "sync_status" {
				device := StringArg(args, "device_id", "")
				op := StringArg(args, "operation_id", "")
				if device == "" || op == "" {
					return "", fmt.Errorf("device_id and operation_id required")
				}
				call = func(ctx context.Context, sc *brain.Client) error {
					_, err := sc.Sync().Diff(ctx, device, op)
					return err
				}
				if toolName == "sync_reconcile" {
					snapshot := StringArg(args, "snapshot", "")
					action := StringArg(args, "action", "")
					raw := StringArg(args, "raw", "")
					if snapshot == "" || (action != "discard" && action != "rebase" && action != "merge") || (action == "merge" && raw == "") {
						return "", fmt.Errorf("snapshot, valid action, and raw for merge required")
					}
					// raw is always sent (empty unless merging), as before.
					req := brain.SyncReconcileRequest{Snapshot: snapshot, Action: brain.SyncReconcileRequestAction(action), Raw: &raw}
					call = func(ctx context.Context, sc *brain.Client) error {
						_, err := sc.Sync().Reconcile(ctx, device, op, req, brain.RequestOptions{})
						return err
					}
				}
			}
			out, err := sdkRaw(ctx, client, call)
			if err != nil {
				return "", err
			}
			return string(out), nil
		})
	}
}
