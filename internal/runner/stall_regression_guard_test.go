package runner

import (
	"context"
	"testing"
	"time"
)

// Regression guard for the question-tool idle/steer/stall fix
// (projects/brain-api/report/jc9ky1jn.md, task psuo7186).
//
// The fix added three OpenCode-only code paths inside checkOpencodeIdleStatus:
// the turn-ended transcript probe (Defect A), the queued-steer re-poke (Defect
// B), and the stall abort/recovery (Defect C). None of them must ever reach a
// Pi (or script) task — those executors detect completion via process exit in
// checkRunningTasks/CheckCompletion and have no HTTP session to probe, steer,
// or abort. checkIdleStatus routes on ExecutorType, so a Pi task should never
// enter the OpenCode handler at all.
//
// The pre-existing Pi/mixed tests (TestCheckIdleStatus_PiTask_*,
// TestCheckIdleStatus_MixedWorkload_BothExecutorTypes) run under default
// config and do not set StallTimeout, PendingSteer, or an ancient
// LastActivity — so they would still pass even if the stall/turn-ended
// machinery leaked into the Pi branch. These tests set exactly those
// conditions and assert the OpenCode-only indirections are never invoked.

// TestRegressionGuard_PiTask_StallMachineryNeverInvoked wires a Pi task under
// the precise conditions that trip the stall + turn-ended + steer-flush paths
// for an OpenCode task — StallTimeout enabled, LastActivity ancient,
// PendingSteer set, pending permissions zero — and asserts none of the
// OpenCode-only indirections (session-history probe, steer flush, session
// abort) fire. If a future refactor drops the ExecutorType switch and sends
// Pi tasks through checkOpencodeIdleStatus, one of these stubs would be
// called and the test fails.
//
// The Pi task is deliberately given a real (busy) port and a SessionID here.
// A real Pi task carries OpencodePort==0, and checkOpencodeIdleStatus's own
// port==0 early-return would mask a broken switch. Populating the port+session
// removes that incidental shield so the ONLY thing keeping the Pi task out of
// the OpenCode stall machinery is the ExecutorType routing switch — which is
// exactly what this test guards.
func TestRegressionGuard_PiTask_StallMachineryNeverInvoked(t *testing.T) {
	// A busy status server: if the Pi task were (wrongly) routed to the
	// OpenCode handler with this port, it would enter the busy branch and
	// hit the probe/stall/steer indirections below.
	server := busyStatusServer(t, "ses_pi")
	defer server.Close()
	port := serverPort(t, server)

	// Any invocation of these OpenCode-only indirections for a Pi task is a
	// regression. The history stub also fails the test if called.
	histRec := &struct{ called bool }{}
	prevHist := sessionHistoryForPort
	sessionHistoryForPort = func(int, string) ([]byte, error) {
		histRec.called = true
		// not-turn-ended, so a wrongly-routed task would proceed to the stall path
		return transcriptJSON(t, msgJSON(t, "assistant", 2000, 0)), nil
	}
	t.Cleanup(func() { sessionHistoryForPort = prevHist })

	flushRec := stubSteerFlusher(t, nil)
	abortRec := stubSessionAborter(t, nil)
	stubPendingPermissions(t, 0)

	// StallTimeout enabled; a live bridge client so the abort gate (which
	// otherwise refuses to abort without one) is satisfied — proving the Pi
	// task is skipped for routing reasons, not because the abort was gated.
	stallMs := 60000
	tr, pm, client := stallTestRunner(t, stallMs)

	proc := newMockProcess(200)
	piTask := RunningTask{
		ID:             "pi-stall1",
		Path:           "projects/proj-a/task/pi-stall1.md",
		Title:          "Pi Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            200,
		StartedAt:      time.Now(),
		ExecutorType:   "pi",
		CompleteOnIdle: true,
		// Real port + session so the OpenCode handler's own port==0 shortcut
		// can't mask a broken routing switch (see doc comment).
		OpencodePort: port,
		SessionID:    "ses_pi",
		// Conditions that WOULD trip the OpenCode stall/steer paths.
		PendingSteer: true,
		LastActivity: time.Now().UTC().Add(-2 * time.Duration(stallMs) * time.Millisecond),
	}
	pm.Add(piTask.ID, piTask, proc)

	tr.checkIdleStatus(context.Background())

	if histRec.called {
		t.Error("turn-ended transcript probe must NOT run for a Pi task")
	}
	if flushRec.called {
		t.Error("steer flush (postEmptyPrompt) must NOT run for a Pi task")
	}
	if abortRec.called {
		t.Error("session abort must NOT run for a Pi task")
	}

	// The Pi task must remain tracked and untouched: no idle timer, no
	// stall recovery flag, no status update.
	info := pm.Get(piTask.ID)
	if info == nil {
		t.Fatal("running Pi task should still be tracked")
	}
	if info.Task.IdleSince != "" {
		t.Errorf("Pi task IdleSince should stay empty, got %q", info.Task.IdleSince)
	}
	if info.Task.StallRecovered {
		t.Error("Pi task must never be marked StallRecovered")
	}
	if info.Task.PendingSteer != true {
		t.Error("Pi task PendingSteer must be left untouched (steer flush is OpenCode-only)")
	}
	for _, u := range client.getUpdateStatusCalls() {
		if u.TaskPath == piTask.Path {
			t.Errorf("Pi task must not get a status update from idle detection, got: %+v", u)
		}
	}
	for _, a := range client.appendCalls {
		if a.TaskPath == piTask.Path && containsMarker(a.Content) {
			t.Error("Pi task must never receive a stall marker append")
		}
	}
}

// TestRegressionGuard_MixedWorkload_OnlyOpencodeStalls proves per-task routing
// under a mixed workload: in a single checkIdleStatus tick, a stalled OpenCode
// task is recovered (abort fires, marker appended) while a Pi task under the
// identical stall-triggering conditions is left completely alone. This locks
// in that the fix did not couple the two executors' idle paths.
func TestRegressionGuard_MixedWorkload_OnlyOpencodeStalls(t *testing.T) {
	// OpenCode side: busy status + NOT-turn-ended transcript so the stall
	// path (not the turn-ended path) is the one exercised.
	server := busyStatusServer(t, "ses_oc")
	defer server.Close()
	port := serverPort(t, server)

	// sessionHistoryForPort is shared; both tasks would route through it if
	// the Pi branch leaked. Return not-turn-ended so the OpenCode task hits
	// the stall path. Record which sessionIDs were probed.
	var probedSessions []string
	prevHist := sessionHistoryForPort
	sessionHistoryForPort = func(_ int, sid string) ([]byte, error) {
		probedSessions = append(probedSessions, sid)
		return transcriptJSON(t, msgJSON(t, "assistant", 2000, 0)), nil
	}
	t.Cleanup(func() { sessionHistoryForPort = prevHist })

	stubPendingPermissions(t, 0)
	abortRec := stubSessionAborter(t, nil)

	stallMs := 60000
	tr, pm, client := stallTestRunner(t, stallMs)

	ancient := time.Now().UTC().Add(-2 * time.Duration(stallMs) * time.Millisecond)

	// Stalled OpenCode task.
	ocProc := newMockProcess(100)
	ocTask := steerTask(port, "ses_oc", false)
	ocTask.ID = "oc-stall1"
	ocTask.Path = "projects/proj-a/task/oc-stall1.md"
	ocTask.ExecutorType = "opencode"
	ocTask.CompleteOnIdle = true
	ocTask.LastActivity = ancient
	pm.Add(ocTask.ID, ocTask, ocProc)

	// Pi task under identical stall-triggering conditions. Given a real
	// port+session (a real Pi task has neither) so the OpenCode handler's
	// port==0 shortcut can't mask a broken routing switch: the ExecutorType
	// switch is the only thing keeping it out of the stall machinery.
	piProc := newMockProcess(200)
	piTask := RunningTask{
		ID:             "pi-stall2",
		Path:           "projects/proj-a/task/pi-stall2.md",
		Title:          "Pi Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            200,
		StartedAt:      time.Now(),
		ExecutorType:   "pi",
		CompleteOnIdle: true,
		OpencodePort:   port,
		SessionID:      "ses_pi",
		LastActivity:   ancient,
	}
	pm.Add(piTask.ID, piTask, piProc)

	tr.checkIdleStatus(context.Background())

	// OpenCode task recovered: abort fired against its session, marker appended.
	if !abortRec.called {
		t.Fatal("expected the stalled OpenCode task to be aborted")
	}
	if abortRec.sessionID != "ses_oc" {
		t.Errorf("abort session = %q, want ses_oc", abortRec.sessionID)
	}
	ocFoundMarker := false
	for _, a := range client.appendCalls {
		if a.TaskPath == ocTask.Path && containsMarker(a.Content) {
			ocFoundMarker = true
		}
	}
	if !ocFoundMarker {
		t.Errorf("expected stall marker append for OpenCode task, got %+v", client.appendCalls)
	}

	// Pi task never probed: only the OpenCode session should appear.
	for _, sid := range probedSessions {
		if sid != "ses_oc" {
			t.Errorf("transcript probe ran for unexpected session %q (Pi task must not be probed)", sid)
		}
	}

	// Pi task untouched.
	piInfo := pm.Get(piTask.ID)
	if piInfo == nil {
		t.Fatal("running Pi task should still be tracked")
	}
	if piInfo.Task.StallRecovered {
		t.Error("Pi task must never be marked StallRecovered")
	}
	for _, a := range client.appendCalls {
		if a.TaskPath == piTask.Path {
			t.Errorf("Pi task must never receive a marker append, got %+v", a)
		}
	}
}
