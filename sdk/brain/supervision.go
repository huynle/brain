package brain

import (
	"context"
	"encoding/json"
	"net/url"
)

// SupervisionService is the supervisor surface: bounded reads (capabilities,
// snapshot, dispatch preview, checkpoints, budgets, operation receipts) and
// admin commands. SubmitOperation drives agents and tasks; none of it is
// script-exposed.
//
// Command documents (SupervisorOperationRequest, SupervisorCheckpointCommand,
// ExecutionBudgetCommand) are json.RawMessage in Go and are sent verbatim: the
// server validates them strictly (unknown fields and wrong types are refused
// with its message), so no client-side copy of that validation can drift.
type SupervisionService struct{ c *Client }

func (c *Client) Supervision() SupervisionService { return SupervisionService{c} }

func (s SupervisionService) Capabilities(ctx context.Context) (*SupervisorCapabilities, error) {
	return result[SupervisorCapabilities](s.c, ctx, "GET", "/supervision/capabilities", nil, nil, RequestOptions{})
}

// Snapshot is a bounded, non-atomic projection; query carries project_id
// (required), task_id, feature_id, after_task and limit.
func (s SupervisionService) Snapshot(ctx context.Context, query url.Values) (*SupervisorSnapshot, error) {
	return result[SupervisorSnapshot](s.c, ctx, "GET", "/supervision/snapshot", nil, query, RequestOptions{})
}

// DispatchPreview reserves nothing; query carries project_id, task_id and manual.
func (s SupervisionService) DispatchPreview(ctx context.Context, query url.Values) (*DispatchPreview, error) {
	return result[DispatchPreview](s.c, ctx, "GET", "/supervision/dispatch-preview", nil, query, RequestOptions{})
}

// SubmitOperation submits a prompt, contextual resume or trigger under an
// idempotency id. Reusing the id with the same document returns the durable
// receipt; it is never automatically retried.
func (s SupervisionService) SubmitOperation(ctx context.Context, command SupervisorOperationRequest, o RequestOptions) (*SupervisorOperation, error) {
	if err := commandDocument(command); err != nil {
		return nil, err
	}
	return result[SupervisorOperation](s.c, ctx, "POST", "/supervision/operations", command, nil, o)
}

// GetOperation reads a receipt of the submitting principal; it never resends.
func (s SupervisionService) GetOperation(ctx context.Context, id string) (*SupervisorOperation, error) {
	return result[SupervisorOperation](s.c, ctx, "GET", "/supervision/operations/"+url.PathEscape(id), nil, nil, RequestOptions{})
}

// Checkpoints lists a project's checkpoints (query project, after) or one
// checkpoint's versions (query project, id).
func (s SupervisionService) Checkpoints(ctx context.Context, query url.Values) (*SupervisorCheckpointList, error) {
	return result[SupervisorCheckpointList](s.c, ctx, "GET", "/supervision/checkpoints", nil, query, RequestOptions{})
}
func (s SupervisionService) UpdateCheckpoint(ctx context.Context, command SupervisorCheckpointCommand, o RequestOptions) (*SupervisorCheckpoint, error) {
	if err := commandDocument(command); err != nil {
		return nil, err
	}
	return result[SupervisorCheckpoint](s.c, ctx, "POST", "/supervision/checkpoints", command, nil, o)
}

// Budget reads one budget (query project, id) and its current window.
func (s SupervisionService) Budget(ctx context.Context, query url.Values) (*ExecutionBudgetStatus, error) {
	return result[ExecutionBudgetStatus](s.c, ctx, "GET", "/supervision/budgets", nil, query, RequestOptions{})
}
func (s SupervisionService) UpdateBudget(ctx context.Context, command ExecutionBudgetCommand, o RequestOptions) (*ExecutionBudgetResult, error) {
	if err := commandDocument(command); err != nil {
		return nil, err
	}
	return result[ExecutionBudgetResult](s.c, ctx, "POST", "/supervision/budgets", command, nil, o)
}

// SendDeliveryCommand is tasks.verifyDelivery with a caller-authored command
// document (configure, verify or integration) sent verbatim, for callers that
// forward a document they did not author; VerifyDelivery is the typed form.
func (s TasksService) SendDeliveryCommand(ctx context.Context, project, id string, command json.RawMessage, o RequestOptions) (*DeliveryUpdateResponse, error) {
	if err := commandDocument(command); err != nil {
		return nil, err
	}
	return result[DeliveryUpdateResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/delivery", command, nil, o)
}

// commandDocument refuses a missing document, which would otherwise be sent
// as JSON null.
func commandDocument(command json.RawMessage) error {
	if len(command) == 0 {
		return &Error{Code: "invalid_request"}
	}
	return nil
}

// SessionTail reads a bounded projection of a session's visible text and
// tool output through the runner bridge (control:* scope); query carries
// after, limit and max_bytes.
func (s RemoteControlService) SessionTail(ctx context.Context, runnerID, sessionID string, query url.Values) (*SessionTailPage, error) {
	return result[SessionTailPage](s.c, ctx, "GET", "/control/runners/"+url.PathEscape(runnerID)+"/sessions/"+url.PathEscape(sessionID)+"/tail", nil, query, RequestOptions{})
}

// SessionDescendants reads persisted child-session linkage (control:* scope);
// query carries after and limit.
func (s RemoteControlService) SessionDescendants(ctx context.Context, runnerID, sessionID string, query url.Values) (*SessionChildrenPage, error) {
	return result[SessionChildrenPage](s.c, ctx, "GET", "/control/runners/"+url.PathEscape(runnerID)+"/sessions/"+url.PathEscape(sessionID)+"/descendants", nil, query, RequestOptions{})
}
