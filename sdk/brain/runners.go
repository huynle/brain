package brain

import (
	"context"
	"encoding/json"
	"net/url"
)

// RunnersService reads the runner registry and the dispatch pause dials.
type RunnersService struct{ c *Client }

func (c *Client) Runners() RunnersService { return RunnersService{c} }

// Status reports the per-project pause dials. Paused/AutomationsPaused are
// true when ANY project is paused on that axis, not a global switch.
func (s RunnersService) Status(ctx context.Context) (*RunnerStatusResponse, error) {
	return result[RunnerStatusResponse](s.c, ctx, "GET", "/tasks/runner/status", nil, nil, RequestOptions{})
}
func (s RunnersService) List(ctx context.Context) (*RunnerListResponse, error) {
	return result[RunnerListResponse](s.c, ctx, "GET", "/runners", nil, nil, RequestOptions{})
}
func (s RunnersService) Get(ctx context.Context, runnerID string) (*RunnerInfo, error) {
	return result[RunnerInfo](s.c, ctx, "GET", "/runners/"+url.PathEscape(runnerID), nil, nil, RequestOptions{})
}
func (s RunnersService) Instances(ctx context.Context, runnerID string) (*InstanceListResponse, error) {
	return result[InstanceListResponse](s.c, ctx, "GET", "/runners/"+url.PathEscape(runnerID)+"/instances", nil, nil, RequestOptions{})
}
func (s RunnersService) AllInstances(ctx context.Context) (*InstanceListResponse, error) {
	return result[InstanceListResponse](s.c, ctx, "GET", "/instances", nil, nil, RequestOptions{})
}

// DispatchService turns the server-wide dispatch dials. Every method writes
// shared state and notifies connected runners; it holds or releases NEW
// dispatch only. No request body is sent (the handlers read none).
type DispatchService struct{ c *Client }

func (c *Client) Dispatch() DispatchService { return DispatchService{c} }

func (s DispatchService) dial(ctx context.Context, path string, o RequestOptions) (*SuccessResponse, error) {
	return result[SuccessResponse](s.c, ctx, "POST", path, nil, nil, o)
}

// PauseAll pauses MANUAL task dispatch for every known project.
func (s DispatchService) PauseAll(ctx context.Context, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/pause", o)
}

// ResumeAll resumes MANUAL task dispatch for every known project.
func (s DispatchService) ResumeAll(ctx context.Context, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/resume", o)
}
func (s DispatchService) PauseProject(ctx context.Context, project string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/pause/"+url.PathEscape(project), o)
}
func (s DispatchService) ResumeProject(ctx context.Context, project string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/resume/"+url.PathEscape(project), o)
}
func (s DispatchService) PauseFeature(ctx context.Context, project, feature string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/features/pause/"+url.PathEscape(project)+"/"+url.PathEscape(feature), o)
}
func (s DispatchService) ResumeFeature(ctx context.Context, project, feature string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/features/resume/"+url.PathEscape(project)+"/"+url.PathEscape(feature), o)
}
func (s DispatchService) PauseProjectAutomations(ctx context.Context, project string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/automations/pause/"+url.PathEscape(project), o)
}
func (s DispatchService) ResumeProjectAutomations(ctx context.Context, project string, o RequestOptions) (*SuccessResponse, error) {
	return s.dial(ctx, "/tasks/runner/automations/resume/"+url.PathEscape(project), o)
}

// RemoteControlService is remote control of runner hosts (control:* scope):
// prompting, aborting and answering permissions in OpenCode sessions, and
// spawning/killing ad-hoc instances. These are real side effects on another
// machine; nothing here is retried.
type RemoteControlService struct{ c *Client }

func (c *Client) RemoteControl() RemoteControlService { return RemoteControlService{c} }

func controlSessionPath(runnerID, instanceID, sessionID string) string {
	return "/control/runners/" + url.PathEscape(runnerID) + "/instances/" + url.PathEscape(instanceID) + "/sessions/" + url.PathEscape(sessionID)
}

// proxied returns the instance's own response body: opaque JSON (OpenCode
// answers abort/permission with a bare true) or empty (prompt_async is 204).
func (s RemoteControlService) proxied(ctx context.Context, path string, body any, o RequestOptions) (json.RawMessage, error) {
	var raw []byte
	if err := s.c.request(ctx, "POST", path, body, nil, o, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 && !json.Valid(raw) {
		return nil, &Error{Code: "invalid_response"}
	}
	return json.RawMessage(raw), nil
}

// SendPrompt starts agent work in a remote session; the agent may run tools
// and call model providers. Output streams separately.
func (s RemoteControlService) SendPrompt(ctx context.Context, runnerID, instanceID, sessionID string, r ControlPromptRequest, o RequestOptions) (json.RawMessage, error) {
	return s.proxied(ctx, controlSessionPath(runnerID, instanceID, sessionID)+"/prompt", r, o)
}
func (s RemoteControlService) AbortSession(ctx context.Context, runnerID, instanceID, sessionID string, o RequestOptions) (json.RawMessage, error) {
	return s.proxied(ctx, controlSessionPath(runnerID, instanceID, sessionID)+"/abort", nil, o)
}

// RespondPermission forwards OpenCode's permission vocabulary untouched;
// once/always let the agent proceed with the requested tool call.
func (s RemoteControlService) RespondPermission(ctx context.Context, runnerID, instanceID, sessionID, permissionID string, r ControlPermissionRequest, o RequestOptions) (json.RawMessage, error) {
	return s.proxied(ctx, controlSessionPath(runnerID, instanceID, sessionID)+"/permissions/"+url.PathEscape(permissionID), r, o)
}

// SpawnInstance starts an ad-hoc OpenCode process on the runner host.
func (s RemoteControlService) SpawnInstance(ctx context.Context, runnerID string, r SpawnInstanceSpec, o RequestOptions) (*ControlSpawnResponse, error) {
	return result[ControlSpawnResponse](s.c, ctx, "POST", "/control/runners/"+url.PathEscape(runnerID)+"/instances", r, nil, o)
}

// KillInstance terminates an ad-hoc instance; task-owned instances are refused (409).
func (s RemoteControlService) KillInstance(ctx context.Context, runnerID, instanceID string, o RequestOptions) (*SuccessResponse, error) {
	return result[SuccessResponse](s.c, ctx, "DELETE", "/control/runners/"+url.PathEscape(runnerID)+"/instances/"+url.PathEscape(instanceID), nil, nil, o)
}

// DispatchLease reads a task's push-dispatch lease (not_found when none).
func (s TasksService) DispatchLease(ctx context.Context, project, id string) (*DispatchLease, error) {
	return result[DispatchLease](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/dispatch-lease", nil, nil, RequestOptions{})
}

// PlacementReasons lists scheduler REJECTIONS only; acceptances leave no row.
func (s TasksService) PlacementReasons(ctx context.Context, project, id string) (*PlacementReasonListResponse, error) {
	return result[PlacementReasonListResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/placement-reasons", nil, nil, RequestOptions{})
}

type SchedulerService struct{ c *Client }

func (c *Client) Scheduler() SchedulerService { return SchedulerService{c} }
func (s SchedulerService) Status(ctx context.Context) (*SchedulerStatus, error) {
	return result[SchedulerStatus](s.c, ctx, "GET", "/scheduler/status", nil, nil, RequestOptions{})
}
