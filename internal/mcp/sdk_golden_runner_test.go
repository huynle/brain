package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huynle/brain-api/internal/bridge"
)

// apiDo performs one seeding request against the real REST API.
func apiDo(t *testing.T, method, url string, body any) {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: status %d", method, url, resp.StatusCode)
	}
}

// fakeRunner is a runner bridge connection served by the test itself: the
// API's real bridge hub tunnels control requests to it over a real
// websocket, and it answers with canned OpenCode-shaped responses. Nothing
// is spawned, prompted or killed anywhere.
type fakeRunner struct {
	mu     sync.Mutex
	frames []string
}

func (f *fakeRunner) record(fr bridge.Frame) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := fr.Type
	switch fr.Type {
	case bridge.FrameReq:
		line += fmt.Sprintf(" %s %s instance=%s body=%s", fr.Method, fr.Path, fr.InstanceID, string(fr.Body))
	case bridge.FrameSpawn:
		spec, _ := json.Marshal(fr.Spec)
		line += " spec=" + string(spec)
	case bridge.FrameKill:
		line += " instance=" + fr.InstanceID
	}
	f.frames = append(f.frames, line)
}

// drain returns and clears the frames received since the last drain.
func (f *fakeRunner) drain() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.frames
	f.frames = nil
	return out
}

// respond is what a well-behaved runner sends back. OpenCode answers
// prompt_async with 204 and an empty body, abort and permission replies with
// a JSON `true`.
func fakeRunnerRespond(fr bridge.Frame) bridge.Frame {
	res := bridge.Frame{Type: bridge.FrameRes, ID: fr.ID}
	switch fr.Type {
	case bridge.FrameReq:
		switch {
		case strings.Contains(fr.Path, "/session/ses-missing/"):
			res.Error = "unknown instance " + fr.InstanceID
		case strings.HasSuffix(fr.Path, "/prompt_async"):
			res.Status = http.StatusNoContent
		default:
			res.Status = http.StatusOK
			res.Body = json.RawMessage(`true`)
		}
	case bridge.FrameSpawn:
		if strings.HasPrefix(fr.Spec.Workdir, "/forbidden") {
			res.Error = "workdir is outside the allowlist"
			break
		}
		inst, _ := json.Marshal(map[string]any{
			"instance_id": "ins-spawned", "runner_id": "golden-runner-1", "kind": "adhoc",
			"status": "starting", "workdir": fr.Spec.Workdir, "title": fr.Spec.Title,
		})
		res.Body = inst
	case bridge.FrameKill:
		if fr.InstanceID == "ins-gone" {
			res.Error = "unknown instance ins-gone"
		}
	}
	return res
}

func connectFakeRunner(t *testing.T, apiURL, runnerID string) *fakeRunner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wsURL := "ws" + strings.TrimPrefix(apiURL, "http") + "/api/v1/runners/" + runnerID + "/bridge"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		t.Fatalf("bridge dial: %v", err)
	}
	conn.SetReadLimit(bridge.MaxFrameBytes)
	f := &fakeRunner{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var fr bridge.Frame
			if json.Unmarshal(data, &fr) != nil {
				continue
			}
			if fr.Type != bridge.FrameReq && fr.Type != bridge.FrameSpawn && fr.Type != bridge.FrameKill {
				continue
			}
			f.record(fr)
			out, _ := json.Marshal(fakeRunnerRespond(fr))
			if conn.Write(ctx, websocket.MessageText, out) != nil {
				return
			}
		}
	}()
	hello, _ := json.Marshal(bridge.Frame{Type: bridge.FrameHello, RunnerID: runnerID, Proto: 1})
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		cancel()
		<-done
	})
	// Wait until the hub reports the connection (runner list decoration).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(apiURL + "/api/v1/runners")
		if err == nil {
			var list struct {
				Runners []struct {
					RunnerID        string `json:"runner_id"`
					BridgeConnected bool   `json:"bridge_connected"`
				} `json:"runners"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&list)
			resp.Body.Close()
			for _, r := range list.Runners {
				if r.RunnerID == runnerID && r.BridgeConnected {
					return f
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runner %s bridge never connected", runnerID)
	return nil
}

func (g *golden) frames(f *fakeRunner) {
	for _, line := range f.drain() {
		g.note("--- runner received: %s", line)
	}
}

// TestGolden_RunnerAndControlTools covers runner_tools.go and
// control_tools.go on a dedicated server: pause-all and the runner registry
// are server-wide, and must never touch another test's (or a real) server.
func TestGolden_RunnerAndControlTools(t *testing.T) {
	api := dedicatedAPI(t)
	g := newGoldenAt(t, "runner_control_tools", api)
	g.scrubRe(`- Last heartbeat: .*`, "- Last heartbeat: <TIME>")
	g.scrubRe(`- Registered: .*`, "- Registered: <TIME>")

	g.call("status empty", "runner_status", nil)
	g.call("runners empty", "runners", nil)
	g.call("instances all empty", "runner_instances_all", nil)

	for _, p := range []string{"alpha", "beta"} {
		seedTask(t, api, p, "Seed "+p)
	}
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{
		"runner_id": "golden-runner-1", "hostname": "host-a", "projects": []string{"alpha"},
		"executors": []string{"opencode"}, "capabilities": []string{"gpu"}, "max_parallel": 3,
	})
	time.Sleep(5 * time.Millisecond) // runners list newest-registered first (ms)
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{
		"runner_id": "golden-runner-2", "hostname": "host-b", "executors": []string{"pi"}, "max_parallel": 1,
	})
	apiDo(t, "PUT", api+"/api/v1/runners/golden-runner-1/instances/ins-adhoc", map[string]any{
		"kind": "adhoc", "status": "idle", "workdir": "/srv/work", "port": 4100, "pid": 4242,
		"started_at": 1700000000000, "last_seen": 1700000005000, "title": "Scratch", "executor": "opencode",
	})
	apiDo(t, "PUT", api+"/api/v1/runners/golden-runner-1/instances/ins-task", map[string]any{
		"kind": "task", "status": "busy", "project_id": "alpha", "task_id": "t1", "feature_id": "f1",
		"agent": "tdd-dev", "model": "m1", "started_at": 1600000000000, "last_seen": 1600000005000,
	})
	apiDo(t, "PUT", api+"/api/v1/runners/golden-runner-2/instances/ins-pi", map[string]any{
		"kind": "task", "status": "starting", "project_id": "beta", "started_at": 1500000000000, "last_seen": 1500000005000,
	})

	g.call("runners", "runners", nil)
	g.call("runners online executor pi", "runners", map[string]any{"status": "online", "executor": "pi"})
	g.call("runners project alpha limit 1", "runners", map[string]any{"project": "alpha", "limit": 1})
	g.call("runners offline", "runners", map[string]any{"status": "offline"})
	g.call("runner get", "runner_get", map[string]any{"runner_id": "golden-runner-1"})
	g.call("runner get alias", "runner_get", map[string]any{"runnerId": "golden-runner-2"})
	g.call("runner get missing", "runner_get", nil)
	g.call("runner get unknown", "runner_get", map[string]any{"runner_id": "nope"})
	g.call("instances", "runner_instances", map[string]any{"runner_id": "golden-runner-1"})
	g.call("instances kind task", "runner_instances", map[string]any{"runner_id": "golden-runner-1", "kind": "task"})
	g.call("instances missing", "runner_instances", nil)
	g.call("instances unknown runner", "runner_instances", map[string]any{"runner_id": "nope"})
	g.call("instances all", "runner_instances_all", nil)
	g.call("instances all filtered", "runner_instances_all", map[string]any{"runner_id": "golden-runner-2", "status": "starting", "project": "beta"})

	g.call("pause project", "runner_pause_project", map[string]any{"project": "alpha"})
	g.call("pause automations", "runner_pause_project_automations", map[string]any{"project": "beta"})
	g.call("status asymmetric", "runner_status", nil)
	g.call("status alpha", "runner_status", map[string]any{"project": "alpha"})
	g.call("status beta", "runner_status", map[string]any{"project": "beta"})
	g.call("pause automations alpha", "runner_pause_project_automations", map[string]any{"project": "alpha"})
	g.call("status alpha both", "runner_status", map[string]any{"project": "alpha"})
	g.call("pause feature missing feature", "runner_pause_feature", map[string]any{"project": "alpha"})
	g.call("pause feature", "runner_pause_feature", map[string]any{"project": "alpha", "feature_id": "f1"})
	g.call("resume feature", "runner_resume_feature", map[string]any{"project": "alpha", "feature_id": "f1"})
	g.call("resume automations", "runner_resume_project_automations", map[string]any{"project": "beta"})
	g.call("resume automations alpha", "runner_resume_project_automations", map[string]any{"project": "alpha"})
	g.call("resume project", "runner_resume_project", map[string]any{"project": "alpha"})
	g.call("status none", "runner_status", map[string]any{"project": "alpha"})
	g.call("pause all unconfirmed", "runner_pause_all", nil)
	g.call("pause all", "runner_pause_all", map[string]any{"confirm": true})
	g.call("status after pause all", "runner_status", nil)
	g.call("resume all unconfirmed", "runner_resume_all", map[string]any{"confirm": false})
	g.call("resume all", "runner_resume_all", map[string]any{"confirm": true})
	g.call("status after resume all", "runner_status", nil)

	ids := map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-adhoc", "session_id": "ses-1"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range ids {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	g.call("prompt missing ids", "control_send_prompt", map[string]any{"runner_id": "golden-runner-1", "text": "hi"})
	g.call("prompt missing session", "control_send_prompt", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-adhoc", "text": "hi"})
	g.call("prompt blank text", "control_send_prompt", with(map[string]any{"text": "   "}))
	g.call("prompt not connected", "control_send_prompt", with(map[string]any{"text": "hi"}))
	g.call("abort not connected", "control_abort_session", ids)
	g.call("permission bad response", "control_permission", with(map[string]any{"permission_id": "per-1", "response": "allow"}))
	g.call("permission missing id", "control_permission", with(map[string]any{"response": "once"}))
	g.call("spawn missing workdir", "control_spawn_instance", map[string]any{"runner_id": "golden-runner-1"})
	g.call("spawn relative workdir", "control_spawn_instance", map[string]any{"runner_id": "golden-runner-1", "workdir": "rel/dir"})
	g.call("spawn missing runner", "control_spawn_instance", map[string]any{"workdir": "/srv/x"})
	g.call("kill unconfirmed", "control_kill_instance", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-adhoc"})
	g.call("kill missing instance", "control_kill_instance", map[string]any{"runner_id": "golden-runner-1", "confirm": true})
	g.call("kill not connected", "control_kill_instance", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-adhoc", "confirm": true})

	runner := connectFakeRunner(t, api, "golden-runner-1")
	g.call("runner get connected", "runner_get", map[string]any{"runner_id": "golden-runner-1"})
	g.call("instances connected", "runner_instances", map[string]any{"runner_id": "golden-runner-1", "status": "idle"})
	g.call("prompt", "control_send_prompt", with(map[string]any{"text": "  hello there  "}))
	g.frames(runner)
	g.call("prompt with model", "control_send_prompt", with(map[string]any{"text": "go", "agent": "build", "provider_id": "anthropic", "modelID": "claude"}))
	g.frames(runner)
	g.call("prompt model half", "control_send_prompt", with(map[string]any{"text": "go", "provider_id": "anthropic"}))
	g.frames(runner)
	g.call("prompt unknown session", "control_send_prompt", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-adhoc", "session_id": "ses-missing", "text": "x"})
	g.frames(runner)
	g.call("abort", "control_abort_session", ids)
	g.frames(runner)
	g.call("permission", "control_permission", with(map[string]any{"permissionId": "per-1", "response": "always"}))
	g.frames(runner)
	g.call("spawn", "control_spawn_instance", map[string]any{"runner_id": "golden-runner-1", "workdir": "/srv/new", "title": "Fresh", "agent": "build", "model": "m2"})
	g.frames(runner)
	g.call("spawn forbidden", "control_spawn_instance", map[string]any{"runner_id": "golden-runner-1", "workdir": "/forbidden/x"})
	g.frames(runner)
	g.call("kill task instance", "control_kill_instance", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-task", "confirm": true})
	g.frames(runner)
	g.call("kill", "control_kill_instance", map[string]any{"runnerId": "golden-runner-1", "instanceId": "ins-spawned", "confirm": true})
	g.frames(runner)
	g.call("kill unknown", "control_kill_instance", map[string]any{"runner_id": "golden-runner-1", "instance_id": "ins-gone", "confirm": true})
	g.frames(runner)
	g.call("instances after kill", "runner_instances_all", map[string]any{"kind": "adhoc"})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead status", "runner_status", map[string]any{"project": "p"})
	g.callAt(dead, "dead runners", "runners", nil)
	g.callAt(dead, "dead runner get", "runner_get", map[string]any{"runner_id": "r/1"})
	g.callAt(dead, "dead instances", "runner_instances", map[string]any{"runner_id": "r 1"})
	g.callAt(dead, "dead instances all", "runner_instances_all", nil)
	g.callAt(dead, "dead pause project", "runner_pause_project", map[string]any{"project": "p"})
	g.callAt(dead, "dead resume project", "runner_resume_project", map[string]any{"project": "p"})
	g.callAt(dead, "dead pause feature", "runner_pause_feature", map[string]any{"project": "p", "feature_id": "f/1"})
	g.callAt(dead, "dead resume feature", "runner_resume_feature", map[string]any{"project": "p", "feature_id": "f1"})
	g.callAt(dead, "dead pause automations", "runner_pause_project_automations", map[string]any{"project": "p"})
	g.callAt(dead, "dead resume automations", "runner_resume_project_automations", map[string]any{"project": "p"})
	g.callAt(dead, "dead pause all", "runner_pause_all", map[string]any{"confirm": true})
	g.callAt(dead, "dead resume all", "runner_resume_all", map[string]any{"confirm": true})
	g.callAt(dead, "dead prompt", "control_send_prompt", map[string]any{"runner_id": "r", "instance_id": "i", "session_id": "s", "text": "t"})
	g.callAt(dead, "dead abort", "control_abort_session", map[string]any{"runner_id": "r", "instance_id": "i", "session_id": "s"})
	g.callAt(dead, "dead permission", "control_permission", map[string]any{"runner_id": "r", "instance_id": "i", "session_id": "s", "permission_id": "p", "response": "reject"})
	g.callAt(dead, "dead spawn", "control_spawn_instance", map[string]any{"runner_id": "r", "workdir": "/w"})
	g.callAt(dead, "dead kill", "control_kill_instance", map[string]any{"runner_id": "r", "instance_id": "i", "confirm": true})
	g.check()
}
