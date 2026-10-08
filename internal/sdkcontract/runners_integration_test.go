package sdkcontract_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

// exerciseRunnerControlSDK drives the 21 runner/dispatch/control/scheduler
// operations against the real handler graph (isolated SQLite, in-process).
// Dispatch dials are real writes to this fixture's store only; control calls
// stop at the bridge because no runner is connected.
func exerciseRunnerControlSDK(t *testing.T, c *brain.Client) {
	t.Helper()
	ctx, o := context.Background(), brain.RequestOptions{}
	const project = "sdk-runner-dials"
	proj, draft := project, "draft"
	created, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Dial target", Content: "x", Project: &proj, Status: &draft}, o)
	if err != nil {
		t.Fatal(err)
	}

	runners, err := c.Runners().List(ctx)
	if err != nil || runners.Runners == nil || len(*runners.Runners) != 1 || (*runners.Runners)[0].RunnerId != "sdk-fixture-runner" {
		t.Fatalf("runners.list: %+v %v", runners, err)
	}
	one, err := c.Runners().Get(ctx, "sdk-fixture-runner")
	if err != nil || one.Hostname != "fixture" || one.MaxParallel != 2 || one.Status != "online" {
		t.Fatalf("runners.get: %+v %v", one, err)
	}
	if _, err := c.Runners().Get(ctx, "missing-runner"); !isCode(err, "not_found") {
		t.Fatalf("runners.get missing: %v", err)
	}
	inst, err := c.Runners().Instances(ctx, "sdk-fixture-runner")
	if err != nil || inst.Total != 0 {
		t.Fatalf("runners.instances: %+v %v", inst, err)
	}
	if _, err := c.Runners().Instances(ctx, "missing-runner"); !isCode(err, "not_found") {
		t.Fatalf("runners.instances missing: %v", err)
	}
	if all, err := c.Runners().AllInstances(ctx); err != nil || all.Total != 0 {
		t.Fatalf("runners.allInstances: %+v %v", all, err)
	}

	pausedTasks := func() []string {
		t.Helper()
		st, err := c.Runners().Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		if st.PausedProjects != nil {
			out = append(out, *st.PausedProjects...)
		}
		return out
	}
	for _, step := range []struct {
		name string
		call func() (*brain.SuccessResponse, error)
	}{
		{"pauseProject", func() (*brain.SuccessResponse, error) { return c.Dispatch().PauseProject(ctx, project, o) }},
		{"pauseFeature", func() (*brain.SuccessResponse, error) { return c.Dispatch().PauseFeature(ctx, project, "feat-1", o) }},
		{"pauseProjectAutomations", func() (*brain.SuccessResponse, error) { return c.Dispatch().PauseProjectAutomations(ctx, project, o) }},
	} {
		if r, err := step.call(); err != nil || !r.Success {
			t.Fatalf("dispatch.%s: %+v %v", step.name, r, err)
		}
	}
	st, err := c.Runners().Status(ctx)
	if err != nil || !st.Paused || !st.AutomationsPaused || !slices.Contains(*st.PausedProjects, project) ||
		!slices.Contains(*st.AutomationPausedProjects, project) || st.PausedFeatures == nil || !slices.Contains(*st.PausedFeatures, project+"/feat-1") {
		t.Fatalf("runners.status after pause: %+v %v", st, err)
	}
	for _, step := range []func() (*brain.SuccessResponse, error){
		func() (*brain.SuccessResponse, error) { return c.Dispatch().ResumeFeature(ctx, project, "feat-1", o) },
		func() (*brain.SuccessResponse, error) { return c.Dispatch().ResumeProjectAutomations(ctx, project, o) },
		func() (*brain.SuccessResponse, error) { return c.Dispatch().ResumeProject(ctx, project, o) },
	} {
		if r, err := step(); err != nil || !r.Success {
			t.Fatalf("dispatch resume: %+v %v", r, err)
		}
	}
	if got := pausedTasks(); slices.Contains(got, project) {
		t.Fatalf("project still paused: %v", got)
	}
	if r, err := c.Dispatch().PauseAll(ctx, o); err != nil || !r.Success || !slices.Contains(pausedTasks(), project) {
		t.Fatalf("dispatch.pauseAll: %+v %v", r, err)
	}
	if r, err := c.Dispatch().ResumeAll(ctx, o); err != nil || !r.Success || len(pausedTasks()) != 0 {
		t.Fatalf("dispatch.resumeAll: %+v %v %v", r, err, pausedTasks())
	}

	if s, err := c.Scheduler().Status(ctx); err != nil || s.Started {
		t.Fatalf("scheduler.status (loop not started in this fixture): %+v %v", s, err)
	}
	if _, err := c.Tasks().DispatchLease(ctx, project, created.Id); !isCode(err, "not_found") {
		t.Fatalf("tasks.dispatchLease: %v", err)
	}
	if r, err := c.Tasks().PlacementReasons(ctx, project, created.Id); err != nil || r.Total != 0 {
		t.Fatalf("tasks.placementReasons: %+v %v", r, err)
	}
	if _, err := c.Tasks().PlacementReasons(ctx, project, "zzzzzzzz"); !isCode(err, "not_found") {
		t.Fatalf("tasks.placementReasons unknown task: %v", err)
	}

	text := "never delivered"
	bridgeDown := func(name string, err error) {
		t.Helper()
		var e *brain.Error
		if !errors.As(err, &e) || e.Status != 502 || e.Message != "runner bridge not connected" {
			t.Fatalf("control.%s without a connected runner: %v", name, err)
		}
	}
	_, err = c.Control().SendPrompt(ctx, "sdk-fixture-runner", "i", "s", brain.ControlPromptRequest{Text: &text}, o)
	bridgeDown("sendPrompt", err)
	_, err = c.Control().AbortSession(ctx, "sdk-fixture-runner", "i", "s", o)
	bridgeDown("abortSession", err)
	_, err = c.Control().RespondPermission(ctx, "sdk-fixture-runner", "i", "s", "p", brain.ControlPermissionRequest{Response: brain.PermissionReject}, o)
	bridgeDown("respondPermission", err)
	_, err = c.Control().SpawnInstance(ctx, "sdk-fixture-runner", brain.SpawnInstanceSpec{Workdir: "/nonexistent/sdk"}, o)
	bridgeDown("spawnInstance", err)
	_, err = c.Control().KillInstance(ctx, "sdk-fixture-runner", "i", o)
	bridgeDown("killInstance", err)
	if _, err := c.Control().SpawnInstance(ctx, "sdk-fixture-runner", brain.SpawnInstanceSpec{Workdir: "relative"}, o); !isCode(err, "invalid_request") {
		t.Fatalf("control.spawnInstance relative workdir: %v", err)
	}
}

func isCode(err error, code string) bool {
	var e *brain.Error
	return errors.As(err, &e) && e.Code == code
}
