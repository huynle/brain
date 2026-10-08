package brain

import (
	"context"
	"net/url"
)

// MonitorsService creates and deletes template monitor tasks (blocked
// inspector, feature review, dream). A monitor is runnable work: an agent runs
// when it fires. None of these methods is script-exposed.
type MonitorsService struct{ c *Client }

func (c *Client) Monitors() MonitorsService { return MonitorsService{c} }

// Create refuses a duplicate template and scope with conflict (409) and an
// unknown template with invalid_request (400).
func (s MonitorsService) Create(ctx context.Context, r CreateMonitorRequest, o RequestOptions) (*CreateMonitorResult, error) {
	return result[CreateMonitorResult](s.c, ctx, "POST", "/monitors", r, nil, o)
}

// DeleteByScope sends its request as a DELETE body; not_found (404) when no
// monitor matches the template and scope.
func (s MonitorsService) DeleteByScope(ctx context.Context, r DeleteMonitorByScopeRequest, o RequestOptions) (*MonitorDeleteByScopeResponse, error) {
	return result[MonitorDeleteByScopeResponse](s.c, ctx, "DELETE", "/monitors/by-scope", r, nil, o)
}

// RunnerCandidates evaluates registered runners for a standalone task.
// Compatible is durable fit; Available is temporary availability.
func (s TasksService) RunnerCandidates(ctx context.Context, project, id string) (*RunnerCandidatesResponse, error) {
	return result[RunnerCandidatesResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/runner-candidates", nil, nil, RequestOptions{})
}

// ProposedRunnerCandidates evaluates a task specification before it exists;
// nothing is stored.
func (s TasksService) ProposedRunnerCandidates(ctx context.Context, project string, r TaskRunnerCandidatesRequest) (*RunnerCandidatesResponse, error) {
	return result[RunnerCandidatesResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/runner-candidates", r, nil, RequestOptions{})
}

// RunnerCandidates evaluates runners against every unfinished task in a feature.
func (s FeaturesService) RunnerCandidates(ctx context.Context, project, id string) (*RunnerCandidatesResponse, error) {
	return result[RunnerCandidatesResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/runner-candidates", nil, nil, RequestOptions{})
}

// ClientContextService registers client observations. Resolve writes the
// client registry row; the resolved project is not a grant.
type ClientContextService struct{ c *Client }

func (c *Client) ClientContext() ClientContextService { return ClientContextService{c} }
func (s ClientContextService) Resolve(ctx context.Context, r ResolveClientContextRequest, o RequestOptions) (*ResolveClientContextResponse, error) {
	return result[ResolveClientContextResponse](s.c, ctx, "POST", "/context/resolve", r, nil, o)
}

// SyncService reads browser-reported sync state and queues reconciliation
// commands (admin only). Reports are observations, not proof of a clean device.
type SyncService struct{ c *Client }

func (c *Client) Sync() SyncService { return SyncService{c} }
func (s SyncService) Devices(ctx context.Context) (*SyncDevicesResponse, error) {
	return result[SyncDevicesResponse](s.c, ctx, "GET", "/sync/devices", nil, nil, RequestOptions{})
}
func (s SyncService) Diff(ctx context.Context, deviceID, operationID string) (*SyncDiff, error) {
	return result[SyncDiff](s.c, ctx, "GET", "/sync/devices/"+url.PathEscape(deviceID)+"/operations/"+url.PathEscape(operationID)+"/diff", nil, nil, RequestOptions{})
}

// Reconcile queues a command the browser applies after it reconnects (202);
// nothing is written to the server by this call.
func (s SyncService) Reconcile(ctx context.Context, deviceID, operationID string, r SyncReconcileRequest, o RequestOptions) (*SyncReconcileResponse, error) {
	return result[SyncReconcileResponse](s.c, ctx, "POST", "/sync/devices/"+url.PathEscape(deviceID)+"/operations/"+url.PathEscape(operationID)+"/reconcile", r, nil, o)
}
