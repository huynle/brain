package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// =============================================================================
// Types
// =============================================================================

// PiEvent represents a parsed event from Pi's JSONL stdout stream.
type PiEvent struct {
	Type     string            `json:"type"`
	Raw      json.RawMessage   `json:"-"` // full original line
	Messages []json.RawMessage `json:"messages,omitempty"`
	Success  *bool             `json:"success,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// PiRPCProcess manages a Pi subprocess communicating via JSONL over
// stdin/stdout. It handles prompt sending, event parsing, completion
// detection, and process lifecycle.
type PiRPCProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	pid    int

	mu       sync.Mutex
	exited   bool
	exitCode int
	idle     bool // true after agent_end received

	events   chan PiEvent  // buffered channel of parsed events
	readDone chan struct{} // closed when readLoop has drained stdout (EOF)
	done     chan struct{} // closed when process exits
}

// piPromptRequest is the JSON structure written to stdin for prompts.
type piPromptRequest struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// piAbortRequest is the JSON structure written to stdin for abort.
type piAbortRequest struct {
	Type string `json:"type"`
}

// =============================================================================
// Constructor
// =============================================================================

// NewPiRPCProcess starts the given command and wires up stdin/stdout for
// JSONL communication. The command must NOT have been started yet.
// A background goroutine reads events from stdout; another monitors process exit.
func NewPiRPCProcess(cmd *exec.Cmd) (*PiRPCProcess, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	p := &PiRPCProcess{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		pid:      cmd.Process.Pid,
		events:   make(chan PiEvent, 256),
		readDone: make(chan struct{}),
		done:     make(chan struct{}),
	}

	// Launch reader goroutine for stdout JSONL parsing
	go p.readLoop()

	// Launch exit monitor goroutine
	go p.waitLoop()

	return p, nil
}

// =============================================================================
// Sending Commands
// =============================================================================

// SendPrompt writes a prompt request to the Pi process's stdin.
// The message is sent as {"type":"prompt","message":"..."} followed by a newline.
func (p *PiRPCProcess) SendPrompt(message string) error {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return fmt.Errorf("process has exited")
	}

	req := piPromptRequest{
		Type:    "prompt",
		Message: message,
	}
	return p.writeJSON(req)
}

// Abort sends an abort command to the Pi process's stdin.
func (p *PiRPCProcess) Abort() error {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return fmt.Errorf("process has exited")
	}

	req := piAbortRequest{Type: "abort"}
	return p.writeJSON(req)
}

// writeJSON marshals v to JSON and writes it as a single JSONL line to stdin.
func (p *PiRPCProcess) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')

	_, err = p.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// =============================================================================
// State Queries
// =============================================================================

// IsIdle returns true after an agent_end event has been received,
// indicating the agent has finished processing.
func (p *PiRPCProcess) IsIdle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.idle
}

// Events returns a read-only channel that receives parsed events from
// the Pi process stdout stream.
func (p *PiRPCProcess) Events() <-chan PiEvent {
	return p.events
}

// WaitForCompletion blocks until an agent_end event is received or the
// context is cancelled. Returns nil on successful completion (agent_end),
// or an error if the context was cancelled or the process exited unexpectedly.
func (p *PiRPCProcess) WaitForCompletion(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			p.mu.Lock()
			idle := p.idle
			p.mu.Unlock()
			if idle {
				return nil
			}
			return fmt.Errorf("process exited before agent_end (exit code: %d)", p.ExitCode())
		case ev, ok := <-p.events:
			if !ok {
				// Channel closed — process ended
				p.mu.Lock()
				idle := p.idle
				p.mu.Unlock()
				if idle {
					return nil
				}
				return fmt.Errorf("event channel closed before agent_end")
			}
			if ev.Type == "agent_end" {
				return nil
			}
		}
	}
}

// =============================================================================
// Process Interface
// =============================================================================

// PID returns the process ID of the Pi subprocess.
func (p *PiRPCProcess) PID() int {
	return p.pid
}

// Pid implements the Process interface.
func (p *PiRPCProcess) Pid() int {
	return p.pid
}

// Exited returns true if the process has exited.
func (p *PiRPCProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

// ExitCode returns the exit code of the process. Returns -1 if the process
// hasn't exited yet or the exit code couldn't be determined.
func (p *PiRPCProcess) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// Kill sends the specified signal to the Pi process.
// If sig is nil, SIGTERM is sent.
func (p *PiRPCProcess) Kill(sig os.Signal) error {
	if p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}

	p.mu.Lock()
	alreadyExited := p.exited
	p.mu.Unlock()

	if alreadyExited {
		return nil
	}

	if sig == nil {
		sig = syscall.SIGTERM
	}

	err := p.cmd.Process.Signal(sig)
	if err != nil {
		return fmt.Errorf("signal process: %w", err)
	}

	return nil
}

// Done returns a channel that is closed when the process exits.
func (p *PiRPCProcess) Done() <-chan struct{} {
	return p.done
}

// =============================================================================
// Internal goroutines
// =============================================================================

// readLoop reads lines from stdout and parses them as JSONL events.
// Malformed lines are logged and skipped. Runs until stdout is closed (EOF).
func (p *PiRPCProcess) readLoop() {
	defer close(p.readDone)
	defer close(p.events)

	scanner := bufio.NewScanner(p.stdout)
	// Increase buffer for potentially large JSONL lines (1MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// Strip optional \r (JSONL spec: split on \n, strip \r)
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		if len(line) == 0 {
			continue
		}

		var ev PiEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Warn("pi_rpc: malformed JSONL line, skipping",
				"error", err,
				"line", string(line),
			)
			continue
		}

		// Preserve raw line
		raw := make(json.RawMessage, len(line))
		copy(raw, line)
		ev.Raw = raw

		// Detect completion: agent_end marks the agent as idle
		if ev.Type == "agent_end" {
			p.mu.Lock()
			p.idle = true
			p.mu.Unlock()
			slog.Debug("pi_rpc: agent_end received, marking idle")
		}

		// Non-blocking send to events channel
		select {
		case p.events <- ev:
		default:
			slog.Warn("pi_rpc: events channel full, dropping event",
				"type", ev.Type,
			)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("pi_rpc: stdout scanner error", "error", err)
	}
}

// waitLoop calls cmd.Wait() and updates exit status when the process exits.
//
// It first waits for readLoop to hit EOF: cmd.Wait() closes the stdout pipe,
// and calling it while the scanner is still draining races the reader against
// the close — on a fast exit the trailing events (including agent_end) are
// silently lost. os/exec documents that Wait must not be called before all
// pipe reads have completed. EOF arrives when the process exits, so this does
// not delay exit detection.
func (p *PiRPCProcess) waitLoop() {
	<-p.readDone
	err := p.cmd.Wait()

	p.mu.Lock()
	p.exited = true
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
		} else {
			p.exitCode = -1
		}
	} else {
		p.exitCode = 0
	}
	p.mu.Unlock()

	close(p.done)
}
