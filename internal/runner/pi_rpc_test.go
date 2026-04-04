package runner

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Helper: create a PiRPCProcess backed by a mock command (cat)
// =============================================================================

// newTestPiProcess creates a PiRPCProcess backed by `cat`, which echoes stdin
// to stdout. Since we need controlled output, we instead use a simple helper
// script that reads from a named pipe and writes to stdout.
//
// For simpler testing, we use a bash process that outputs predefined JSONL
// and then exits.
func newPiProcessFromScript(t *testing.T, script string) *PiRPCProcess {
	t.Helper()

	cmd := testCommand("bash", "-c", script)
	p, err := NewPiRPCProcess(cmd)
	if err != nil {
		t.Fatalf("NewPiRPCProcess failed: %v", err)
	}
	t.Cleanup(func() {
		p.Kill(nil)
		// Drain done channel to avoid goroutine leak
		select {
		case <-p.Done():
		case <-time.After(2 * time.Second):
		}
	})
	return p
}

// testCommand creates an exec.Cmd, used so tests work without CommandFactory.
func testCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// =============================================================================
// NewPiRPCProcess
// =============================================================================

func TestNewPiRPCProcess_StartsProcess(t *testing.T) {
	// Simple script that prints one event and exits
	p := newPiProcessFromScript(t, `echo '{"type":"agent_start"}'`)

	if p.PID() <= 0 {
		t.Errorf("expected positive PID, got %d", p.PID())
	}

	// Pid() and PID() should return the same value
	if p.Pid() != p.PID() {
		t.Errorf("Pid()=%d != PID()=%d", p.Pid(), p.PID())
	}
}

// =============================================================================
// Event Parsing
// =============================================================================

func TestPiRPCProcess_ParsesJSONLEvents(t *testing.T) {
	script := `
echo '{"type":"agent_start"}'
echo '{"type":"message_update"}'
echo '{"type":"agent_end","messages":[{"role":"assistant"}]}'
`
	p := newPiProcessFromScript(t, script)

	var events []PiEvent
	timeout := time.After(3 * time.Second)

	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				goto done
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	if events[0].Type != "agent_start" {
		t.Errorf("event[0].Type = %q, want %q", events[0].Type, "agent_start")
	}
	if events[1].Type != "message_update" {
		t.Errorf("event[1].Type = %q, want %q", events[1].Type, "message_update")
	}
	if events[2].Type != "agent_end" {
		t.Errorf("event[2].Type = %q, want %q", events[2].Type, "agent_end")
	}

	// agent_end should have messages
	if len(events[2].Messages) != 1 {
		t.Errorf("expected 1 message in agent_end, got %d", len(events[2].Messages))
	}
}

func TestPiRPCProcess_PreservesRawJSON(t *testing.T) {
	script := `echo '{"type":"agent_start","extra":"data"}'`
	p := newPiProcessFromScript(t, script)

	select {
	case ev := <-p.Events():
		if ev.Raw == nil {
			t.Fatal("expected Raw to be set")
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(ev.Raw, &raw); err != nil {
			t.Fatalf("Raw is not valid JSON: %v", err)
		}
		if raw["extra"] != "data" {
			t.Errorf("expected extra=data in Raw, got %v", raw["extra"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPiRPCProcess_SkipsEmptyLines(t *testing.T) {
	// Include empty lines and carriage returns between events
	script := `
echo ''
echo '{"type":"agent_start"}'
printf '\r\n'
echo '{"type":"agent_end"}'
echo ''
`
	p := newPiProcessFromScript(t, script)

	var events []PiEvent
	timeout := time.After(3 * time.Second)

	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				goto done
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	if len(events) != 2 {
		t.Fatalf("expected 2 events (skipping empty lines), got %d: %+v", len(events), events)
	}
}

func TestPiRPCProcess_HandlesMalformedJSON(t *testing.T) {
	// Mix valid and invalid JSON lines
	script := `
echo '{"type":"agent_start"}'
echo 'this is not json'
echo '{"type":"agent_end"}'
`
	p := newPiProcessFromScript(t, script)

	var events []PiEvent
	timeout := time.After(3 * time.Second)

	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				goto done
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	// Malformed line should be skipped, only 2 valid events
	if len(events) != 2 {
		t.Fatalf("expected 2 events (malformed skipped), got %d", len(events))
	}
	if events[0].Type != "agent_start" {
		t.Errorf("event[0].Type = %q, want agent_start", events[0].Type)
	}
	if events[1].Type != "agent_end" {
		t.Errorf("event[1].Type = %q, want agent_end", events[1].Type)
	}
}

// =============================================================================
// IsIdle / Completion Detection
// =============================================================================

func TestPiRPCProcess_IsIdle_FalseInitially(t *testing.T) {
	// Long-running script so we can check idle before any events
	p := newPiProcessFromScript(t, `sleep 5`)

	if p.IsIdle() {
		t.Error("expected IsIdle=false initially")
	}
}

func TestPiRPCProcess_IsIdle_TrueAfterAgentEnd(t *testing.T) {
	script := `
echo '{"type":"agent_start"}'
echo '{"type":"agent_end"}'
`
	p := newPiProcessFromScript(t, script)

	// Wait for process to finish sending events
	<-p.Done()

	// Drain events to ensure readLoop processed agent_end
	for range p.Events() {
	}

	if !p.IsIdle() {
		t.Error("expected IsIdle=true after agent_end")
	}
}

func TestPiRPCProcess_WaitForCompletion_Success(t *testing.T) {
	script := `
echo '{"type":"agent_start"}'
sleep 0.1
echo '{"type":"agent_end"}'
`
	p := newPiProcessFromScript(t, script)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.WaitForCompletion(ctx)
	if err != nil {
		t.Fatalf("WaitForCompletion returned error: %v", err)
	}
}

func TestPiRPCProcess_WaitForCompletion_ContextCancelled(t *testing.T) {
	// Long-running process that never sends agent_end
	p := newPiProcessFromScript(t, `sleep 30`)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := p.WaitForCompletion(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestPiRPCProcess_WaitForCompletion_ProcessExitsBeforeAgentEnd(t *testing.T) {
	// Process that sends start but exits without agent_end
	script := `echo '{"type":"agent_start"}'`

	p := newPiProcessFromScript(t, script)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.WaitForCompletion(ctx)
	if err == nil {
		t.Fatal("expected error when process exits before agent_end")
	}
}

// =============================================================================
// SendPrompt / Abort
// =============================================================================

func TestPiRPCProcess_SendPrompt(t *testing.T) {
	// Use cat to echo stdin to stdout so we can verify what was written
	script := `cat`

	p := newPiProcessFromScript(t, script)

	if err := p.SendPrompt("hello world"); err != nil {
		t.Fatalf("SendPrompt failed: %v", err)
	}

	// Read the echoed event from stdout
	select {
	case ev := <-p.Events():
		if ev.Type != "prompt" {
			t.Errorf("expected type=prompt, got %q", ev.Type)
		}
		// Parse raw to check message field
		var raw map[string]interface{}
		if err := json.Unmarshal(ev.Raw, &raw); err != nil {
			t.Fatalf("failed to parse raw: %v", err)
		}
		if msg, ok := raw["message"].(string); !ok || msg != "hello world" {
			t.Errorf("expected message='hello world', got %v", raw["message"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for echoed prompt")
	}
}

func TestPiRPCProcess_Abort(t *testing.T) {
	script := `cat`

	p := newPiProcessFromScript(t, script)

	if err := p.Abort(); err != nil {
		t.Fatalf("Abort failed: %v", err)
	}

	// Read the echoed abort from stdout
	select {
	case ev := <-p.Events():
		if ev.Type != "abort" {
			t.Errorf("expected type=abort, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for echoed abort")
	}
}

func TestPiRPCProcess_SendPrompt_AfterExit(t *testing.T) {
	script := `echo '{"type":"agent_start"}'`

	p := newPiProcessFromScript(t, script)

	// Wait for process to exit
	<-p.Done()

	err := p.SendPrompt("should fail")
	if err == nil {
		t.Fatal("expected error sending prompt after process exit")
	}
}

// =============================================================================
// Process Lifecycle
// =============================================================================

func TestPiRPCProcess_ExitedAndExitCode(t *testing.T) {
	script := `echo '{"type":"agent_end"}'`

	p := newPiProcessFromScript(t, script)

	// Initially might not be exited yet, wait
	<-p.Done()

	if !p.Exited() {
		t.Error("expected Exited=true after process exits")
	}

	if p.ExitCode() != 0 {
		t.Errorf("expected ExitCode=0, got %d", p.ExitCode())
	}
}

func TestPiRPCProcess_NonZeroExitCode(t *testing.T) {
	script := `exit 42`

	p := newPiProcessFromScript(t, script)

	<-p.Done()

	if !p.Exited() {
		t.Error("expected Exited=true")
	}
	if p.ExitCode() != 42 {
		t.Errorf("expected ExitCode=42, got %d", p.ExitCode())
	}
}

func TestPiRPCProcess_Kill(t *testing.T) {
	// Long-running process to kill
	p := newPiProcessFromScript(t, `sleep 30`)

	err := p.Kill(nil) // nil defaults to SIGTERM in Kill
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// Wait for process to actually exit
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit after kill")
	}

	if !p.Exited() {
		t.Error("expected Exited=true after Kill")
	}
}

func TestPiRPCProcess_Done(t *testing.T) {
	script := `echo done`

	p := newPiProcessFromScript(t, script)

	select {
	case <-p.Done():
		// OK, process exited
	case <-time.After(3 * time.Second):
		t.Fatal("Done() channel not closed after process exit")
	}
}

// =============================================================================
// Concurrent Access
// =============================================================================

func TestPiRPCProcess_ConcurrentAccess(t *testing.T) {
	script := `
echo '{"type":"agent_start"}'
sleep 0.2
echo '{"type":"agent_end"}'
`
	p := newPiProcessFromScript(t, script)

	// Hit IsIdle, Exited, ExitCode, Kill concurrently
	done := make(chan struct{})

	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			_ = p.IsIdle()
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			_ = p.Exited()
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			_ = p.ExitCode()
		}
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent access test timed out")
		}
	}
}

// =============================================================================
// Process Exit Mid-Stream (stdout EOF before agent_end)
// =============================================================================

func TestPiRPCProcess_StdoutEOFBeforeAgentEnd(t *testing.T) {
	// Process sends partial events then crashes
	script := `
echo '{"type":"agent_start"}'
echo '{"type":"message_update"}'
exit 1
`
	p := newPiProcessFromScript(t, script)

	var events []PiEvent
	for ev := range p.Events() {
		events = append(events, ev)
	}

	// Should have received 2 events before EOF
	if len(events) != 2 {
		t.Fatalf("expected 2 events before crash, got %d", len(events))
	}

	// Process should be marked as exited (not idle)
	<-p.Done()
	if p.IsIdle() {
		t.Error("expected IsIdle=false (no agent_end before crash)")
	}
	if !p.Exited() {
		t.Error("expected Exited=true")
	}
	if p.ExitCode() != 1 {
		t.Errorf("expected ExitCode=1, got %d", p.ExitCode())
	}
}
