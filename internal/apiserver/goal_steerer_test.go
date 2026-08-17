package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// fakeInstanceLister returns a scripted instance registry snapshot.
type fakeInstanceLister struct {
	instances []types.OpencodeInstance
	err       error
}

func (f *fakeInstanceLister) ListAllInstances(ctx context.Context) (*types.InstanceListResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &types.InstanceListResponse{Instances: f.instances, Total: len(f.instances)}, nil
}

// fakeBridgeDoer records the proxied request and returns a scripted response.
type fakeBridgeDoer struct {
	runnerID   string
	instanceID string
	method     string
	path       string
	body       []byte

	status int
	err    error
}

func (f *fakeBridgeDoer) Do(ctx context.Context, runnerID, instanceID, method, path string, body []byte) (int, []byte, error) {
	f.runnerID, f.instanceID, f.method, f.path, f.body = runnerID, instanceID, method, path, body
	if f.err != nil {
		return 0, nil, f.err
	}
	status := f.status
	if status == 0 {
		status = 204
	}
	return status, nil, nil
}

func taskInstance(runner, instance, task, executor string, sessions ...string) types.OpencodeInstance {
	return types.OpencodeInstance{
		InstanceID: instance,
		RunnerID:   runner,
		Kind:       types.InstanceKindTask,
		ProjectID:  "proj",
		TaskID:     task,
		SessionIDs: sessions,
		Status:     "busy",
		Executor:   executor,
	}
}

func TestBridgeGoalSteerer_SteersViaPromptAsync(t *testing.T) {
	lister := &fakeInstanceLister{instances: []types.OpencodeInstance{
		taskInstance("runner-1", "inst-1", "task-42", "opencode", "ses_old", "ses_new"),
	}}
	bridge := &fakeBridgeDoer{}
	steerer := newBridgeGoalSteerer(lister, bridge)

	res, err := steerer.SteerTask(context.Background(), "proj", "task-42", "steer prompt")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if !res.Steered {
		t.Fatalf("result = %+v, want Steered", res)
	}
	if bridge.runnerID != "runner-1" || bridge.instanceID != "inst-1" {
		t.Errorf("routed to %s/%s, want runner-1/inst-1", bridge.runnerID, bridge.instanceID)
	}
	// Most recent session wins; prompt goes through prompt_async.
	if bridge.method != "POST" || bridge.path != "/session/ses_new/prompt_async" {
		t.Errorf("request = %s %s, want POST /session/ses_new/prompt_async", bridge.method, bridge.path)
	}
	var payload struct {
		Parts []map[string]any `json:"parts"`
	}
	if err := json.Unmarshal(bridge.body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(payload.Parts) != 1 || payload.Parts[0]["type"] != "text" || payload.Parts[0]["text"] != "steer prompt" {
		t.Errorf("payload parts = %+v, want single text part with the prompt", payload.Parts)
	}
}

func TestBridgeGoalSteerer_NoLiveInstanceSkips(t *testing.T) {
	steerer := newBridgeGoalSteerer(&fakeInstanceLister{}, &fakeBridgeDoer{})

	res, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if res.Steered || res.Unsupported {
		t.Errorf("result = %+v, want graceful skip", res)
	}
	if !strings.Contains(res.Reason, "no live instance") {
		t.Errorf("reason = %q, want no-live-instance skip", res.Reason)
	}
}

func TestBridgeGoalSteerer_UnsupportedExecutor(t *testing.T) {
	lister := &fakeInstanceLister{instances: []types.OpencodeInstance{
		taskInstance("runner-1", "inst-1", "task-42", "pi", "ses_1"),
	}}
	bridge := &fakeBridgeDoer{}
	steerer := newBridgeGoalSteerer(lister, bridge)

	res, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if !res.Unsupported {
		t.Fatalf("result = %+v, want Unsupported for pi executor", res)
	}
	if bridge.method != "" {
		t.Errorf("bridge was called (%s %s) for an unsupported executor", bridge.method, bridge.path)
	}
}

func TestBridgeGoalSteerer_NoSessionYetSkips(t *testing.T) {
	lister := &fakeInstanceLister{instances: []types.OpencodeInstance{
		taskInstance("runner-1", "inst-1", "task-42", "opencode"),
	}}
	steerer := newBridgeGoalSteerer(lister, &fakeBridgeDoer{})

	res, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if res.Steered {
		t.Errorf("result = %+v, want skip while no session is discovered", res)
	}
}

func TestBridgeGoalSteerer_BridgeFailureErrors(t *testing.T) {
	lister := &fakeInstanceLister{instances: []types.OpencodeInstance{
		taskInstance("runner-1", "inst-1", "task-42", "opencode", "ses_1"),
	}}
	steerer := newBridgeGoalSteerer(lister, &fakeBridgeDoer{err: fmt.Errorf("bridge not connected")})

	if _, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt"); err == nil {
		t.Fatal("expected delivery error when the bridge fails")
	}

	// Upstream non-2xx statuses are delivery errors too.
	steerer = newBridgeGoalSteerer(lister, &fakeBridgeDoer{status: 500})
	if _, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt"); err == nil {
		t.Fatal("expected delivery error on upstream 500")
	}
}

func TestBridgeGoalSteerer_ProjectAndExitedFiltering(t *testing.T) {
	other := taskInstance("runner-1", "inst-other", "task-42", "opencode", "ses_1")
	other.ProjectID = "other-proj"
	exited := taskInstance("runner-1", "inst-exited", "task-42", "opencode", "ses_2")
	exited.Status = "exited"
	match := taskInstance("runner-2", "inst-live", "task-42", "opencode", "ses_3")

	lister := &fakeInstanceLister{instances: []types.OpencodeInstance{other, exited, match}}
	bridge := &fakeBridgeDoer{}
	steerer := newBridgeGoalSteerer(lister, bridge)

	res, err := steerer.SteerTask(context.Background(), "proj", "task-42", "prompt")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if !res.Steered {
		t.Fatalf("result = %+v, want Steered via the live matching-project instance", res)
	}
	if bridge.instanceID != "inst-live" {
		t.Errorf("routed to %q, want inst-live (project + liveness filtered)", bridge.instanceID)
	}
}
