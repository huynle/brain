package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/huynle/brain-api/sdk/brain"
)

// RegisterSupervisorTools registers the supervisor tools for MCP discovery.
func RegisterSupervisorTools(s *Server, client *APIClient) {
	registerSessionChildren(s, client)
	registerResourceHealth(s, client)
	registerEventWait(s, client)
	registerDeliveryTools(s, client)
	registerSupervisorReads(s, client)
	registerOperationTools(s, client)
	registerSupervisorLedgers(s, client)
	s.RegisterTool(Tool{Name: "session_tail", Description: "Read bounded visible session text and tool output, live or historical after instance exit. Requires control scope. Excludes reasoning and tool inputs; known credential patterns are redacted. Upsert records by ID. A cursor rechecks the last streaming part; expired cursors return a recent tail. Runner must be connected; executor history support varies.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
		"runner_id": {Type: "string"}, "session_id": {Type: "string"}, "after": {Type: "string"}, "limit": {Type: "number", Description: "1..100 records; default 20"}, "max_bytes": {Type: "number", Description: "1024..65536; default 16384"},
	}, Required: []string{"runner_id", "session_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		runner, session := StringArg(args, "runner_id", ""), StringArg(args, "session_id", "")
		if runner == "" || session == "" {
			return "", fmt.Errorf("runner_id and session_id are required")
		}
		query := legacyQuery(map[string]string{"after": StringArg(args, "after", ""), "limit": strconv.Itoa(IntArg(args, "limit", 20)), "max_bytes": strconv.Itoa(IntArg(args, "max_bytes", 16384))})
		page, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.RemoteControl().SessionTail(ctx, runner, session, query)
			return err
		})
		if err != nil {
			return "", err
		}

		b, err := json.Marshal(page)
		return string(b), err
	})
}

func registerSessionChildren(s *Server, client *APIClient) {
	s.RegisterTool(Tool{Name: "session_children", Description: "Read a bounded page of OpenCode child/descendant linkage (depth 5), including historical sessions. Requires control scope. State and last_activity are explicitly unknown in persisted linkage. Use session_tail per child. Pi child-session discovery is unsupported. Changed trees expire cursors and restart pagination; deduplicate by session_id.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"runner_id": {Type: "string"}, "session_id": {Type: "string"}, "after": {Type: "string"}, "limit": {Type: "number"}}, Required: []string{"runner_id", "session_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		runner, session := StringArg(args, "runner_id", ""), StringArg(args, "session_id", "")
		if runner == "" || session == "" {
			return "", fmt.Errorf("runner_id and session_id are required")
		}
		query := legacyQuery(map[string]string{"limit": strconv.Itoa(IntArg(args, "limit", 20)), "after": StringArg(args, "after", "")})
		page, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.RemoteControl().SessionDescendants(ctx, runner, session, query)
			return err
		})
		if err != nil {
			return "", err
		}
		return string(page), nil
	})
}

func registerResourceHealth(s *Server, client *APIClient) {
	s.RegisterTool(Tool{Name: "resource_health", Description: "Read bounded recent task process-tree RSS and memory-pressure observations for a project. Reuses the existing memory guard; no new sampling or capacity changes. Null is unavailable, never zero. Freshness is explicit; quiet activity is not proof of a stall. Old runners and disabled guards have no samples.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}}, Required: []string{"project"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProjectArg(ctx, args)
		if project == "" {
			return "", fmt.Errorf("project is required")
		}
		query := legacyQuery(map[string]string{"project_id": project, "task_id": StringArg(args, "task_id", "")})
		result, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Events().ResourceHealth(ctx, query)
			return err
		})
		if err != nil {
			return "", err
		}
		return string(result), nil
	})
}

func registerEventWait(s *Server, client *APIClient) {
	s.RegisterTool(Tool{Name: "events_wait", Description: "Wait up to 25 seconds for new project/task/feature events using an opaque reconnect cursor. Initial calls return retained history. Expired cursors explicitly require a fresh state snapshot. Output is bounded; events follow server ring order. No new polling loop. Read scope required.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}, "feature_id": {Type: "string"}, "type": {Type: "string"}, "after": {Type: "string"}, "timeout_ms": {Type: "number"}, "limit": {Type: "number"}}, Required: []string{"project"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProjectArg(ctx, args)
		if project == "" {
			return "", fmt.Errorf("project is required")
		}
		query := map[string]string{"project_id": project, "timeout_ms": strconv.Itoa(IntArg(args, "timeout_ms", 25000)), "limit": strconv.Itoa(IntArg(args, "limit", 100))}
		for _, key := range []string{"task_id", "feature_id", "type", "after"} {
			query[key] = StringArg(args, key, "")
		}
		result, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Events().Wait(ctx, legacyQuery(query))
			return err
		})
		if err != nil {
			return "", err
		}
		return string(result), nil
	})
}

func registerDeliveryTools(s *Server, client *APIClient) {
	s.RegisterTool(Tool{Name: "delivery_gate", Description: "Read implementation status separately from opt-in verified delivery gates, including unmet evidence. Does not merge or deploy.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}}, Required: []string{"project", "task_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, task := ResolveProjectArg(ctx, args), StringArg(args, "task_id", "")
		if project == "" || task == "" {
			return "", fmt.Errorf("project and task_id required")
		}
		out, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Tasks().Delivery(ctx, project, task)
			return err
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	})
	s.RegisterTool(Tool{Name: "delivery_verify", Description: "Read-only GitHub provider verification and durable Brain evidence update for a configured task delivery gate. Requires admin scope and expected_revision. Never merges, pushes or deploys. Provider failure invalidates previous evidence and reports unmet gates.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}, "expected_revision": {Type: "number"}}, Required: []string{"project", "task_id", "expected_revision"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, task := ResolveProjectArg(ctx, args), StringArg(args, "task_id", "")
		if project == "" || task == "" {
			return "", fmt.Errorf("project and task_id required")
		}
		command := brain.DeliveryCommand{Action: brain.Verify, ExpectedRevision: IntArg(args, "expected_revision", -1)}
		out, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Tasks().VerifyDelivery(ctx, project, task, command, brain.RequestOptions{})
			return err
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	})
}

func registerSupervisorReads(s *Server, client *APIClient) {
	for name, path := range map[string]string{"supervisor_capabilities": "capabilities", "supervisor_snapshot": "snapshot", "task_dispatch_preview": "dispatch-preview"} {
		required := []string{}
		if name != "supervisor_capabilities" {
			required = []string{"project"}
		}
		if name == "task_dispatch_preview" {
			required = append(required, "task_id")
		}
		s.RegisterTool(Tool{Name: name, Description: "Read-only structured supervisor " + path + ". Reports unavailable/unknown sources explicitly; does not reserve capacity, create claims, or dispatch work. Requires read scope.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}, "feature_id": {Type: "string"}, "limit": {Type: "number"}, "after_task": {Type: "string"}, "manual": {Type: "boolean"}}, Required: required}}, func(ctx context.Context, args map[string]any) (string, error) {
			query := map[string]string{"project_id": ResolveProjectArg(ctx, args), "limit": strconv.Itoa(IntArg(args, "limit", 50))}
			for _, key := range []string{"task_id", "feature_id", "after_task"} {
				query[key] = StringArg(args, key, "")
			}
			if manual, ok := args["manual"].(bool); ok && manual {
				query["manual"] = "true"
			}
			if path == "capabilities" {
				var out json.RawMessage
				if err := client.Request(ctx, http.MethodGet, "/supervision/"+path, nil, query, &out); err != nil {
					return "", err
				}
				return string(out), nil
			}
			out, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
				var err error
				if path == "snapshot" {
					_, err = sc.Supervision().Snapshot(ctx, legacyQuery(query))
				} else {
					_, err = sc.Supervision().DispatchPreview(ctx, legacyQuery(query))
				}
				return err
			})
			if err != nil {
				return "", err
			}
			return string(out), nil
		})
	}
}

func registerOperationTools(s *Server, client *APIClient) {
	s.RegisterTool(Tool{Name: "supervisor_operation", Description: "Submit an idempotent prompt, contextual resume or trigger. Requires admin scope. Reuse the same ID after a timeout; a different payload with that ID conflicts. Accepted/delivered is not task completion. Unknown outcomes are never automatically replayed.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"id": {Type: "string"}, "operation": {Type: "string", Enum: []string{"prompt", "resume_with_context", "trigger"}}, "project": {Type: "string"}, "task_id": {Type: "string"}, "runner_id": {Type: "string"}, "instance_id": {Type: "string"}, "session_id": {Type: "string"}, "text": {Type: "string"}, "budget_id": {Type: "string"}, "budget_units": {Type: "number"}, "parent_reservation": {Type: "string"}, "checkpoint_id": {Type: "string"}, "checkpoint_revision": {Type: "number"}}, Required: []string{"id", "operation"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		// The arguments are the command document, forwarded verbatim; the
		// server's strict decoding is the only validator.
		document, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		result, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Supervision().SubmitOperation(ctx, document, brain.RequestOptions{})
			return err
		})
		if err != nil {
			return "", err
		}
		return string(result), nil
	})
	s.RegisterTool(Tool{Name: "supervisor_operation_get", Description: "Read a durable operation receipt by ID, scoped to the submitting principal. Does not resend or relaunch anything.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"id": {Type: "string"}}, Required: []string{"id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "", fmt.Errorf("id required")
		}
		result, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Supervision().GetOperation(ctx, id)
			return err
		})
		if err != nil {
			return "", err
		}
		return string(result), nil
	})
}

func registerSupervisorLedgers(s *Server, client *APIClient) {
	for name, path := range map[string]string{"supervisor_checkpoint": "checkpoints", "execution_budget": "budgets"} {
		checkpoints := path == "checkpoints"
		s.RegisterTool(Tool{Name: name, Description: "Read or update the durable " + path + " ledger. GET requires read scope; writes require admin scope. Explicit revisions prevent stale edits. Checkpoint answers are not verification. Budget units cover admitted work only; opaque executor tokens and cost are unknown.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "id": {Type: "string"}, "after": {Type: "string"}, "command": {Type: "object", Description: "Optional REST command object. Checkpoints: action request/answer/verify/supersede, expected_revision, checkpoint {id,project,artifact,question,answer,verification_reference}. Budgets: action configure/reserve/commit/cancel, expected_revision, budget {id,project,timezone,unit,limit}, reservation_id,parent_id,units."}}}}, func(ctx context.Context, args map[string]any) (string, error) {
			query := legacyQuery(map[string]string{"project": ResolveProjectArg(ctx, args), "id": StringArg(args, "id", ""), "after": StringArg(args, "after", "")})
			call := func(ctx context.Context, sc *brain.Client) error {
				var err error
				if checkpoints {
					_, err = sc.Supervision().Checkpoints(ctx, query)
				} else {
					_, err = sc.Supervision().Budget(ctx, query)
				}
				return err
			}
			if command, ok := args["command"].(map[string]any); ok {
				// The command document is forwarded verbatim; the server's
				// strict decoding is the only validator.
				document, err := json.Marshal(command)
				if err != nil {
					return "", fmt.Errorf("marshal body: %w", err)
				}
				call = func(ctx context.Context, sc *brain.Client) error {
					var err error
					if checkpoints {
						_, err = sc.Supervision().UpdateCheckpoint(ctx, document, brain.RequestOptions{})
					} else {
						_, err = sc.Supervision().UpdateBudget(ctx, document, brain.RequestOptions{})
					}
					return err
				}
			}
			result, err := sdkRaw(ctx, client, call)
			if err != nil {
				return "", err
			}
			return string(result), nil
		})
	}
	s.RegisterTool(Tool{Name: "delivery_record", Description: "Configure an opt-in delivery policy or record integration evidence for the exact verified merge artifact. Admin scope and expected_revision required. Provider evidence cannot be supplied by this tool; use delivery_verify.", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string"}, "task_id": {Type: "string"}, "command": {Type: "object", Description: "action configure with policy {required,repository,pull_request,head,target,required_checks}; or action integration with artifact,passed,evidence_reference. Include expected_revision."}}, Required: []string{"project", "task_id", "command"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, task := ResolveProjectArg(ctx, args), StringArg(args, "task_id", "")
		command, ok := args["command"].(map[string]any)
		if project == "" || task == "" || !ok {
			return "", fmt.Errorf("project, task_id and command required")
		}
		document, err := json.Marshal(command)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		result, err := sdkRaw(ctx, client, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Tasks().SendDeliveryCommand(ctx, project, task, document, brain.RequestOptions{})
			return err
		})
		if err != nil {
			return "", err
		}
		return string(result), nil
	})
}
