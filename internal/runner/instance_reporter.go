package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// instanceReporterClient is the optional client surface used to report
// OpenCode instances to the Brain API. Implemented by APIClient; checked via
// type assertion so mock clients in tests don't need it.
type instanceReporterClient interface {
	UpsertInstance(ctx context.Context, runnerID string, inst types.OpencodeInstance) error
	DeleteInstance(ctx context.Context, runnerID, instanceID string) error
}

// generateInstanceID returns a new opaque instance identifier ("inst_" + hex8).
func generateInstanceID() string {
	b := make([]byte, 4)
	// crypto/rand.Read never returns an error as of Go 1.24 — it
	// panics if the system entropy source fails.
	_, _ = rand.Read(b)
	return "inst_" + hex.EncodeToString(b)
}

// runnerHostname returns the local hostname, falling back to "unknown".
func runnerHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// taskInstanceStatus derives the registry status for a tracked task process.
// For OpenCode we use the presence of the API port + IdleSince marker; for
// pi/script the process is either running (busy) or not tracked at all —
// there is no "attached to a session" concept, so we simplify to
// starting/busy/exited based on process lifecycle.
func taskInstanceStatus(info *ProcessInfo) string {
	if info.IsExited || info.Proc.Exited() {
		return types.InstanceStatusExited
	}
	if info.Task.ExecutorType == "opencode" {
		if info.Task.OpencodePort == 0 {
			return types.InstanceStatusStarting
		}
		if info.Task.IdleSince != "" {
			return types.InstanceStatusIdle
		}
		return types.InstanceStatusBusy
	}
	// pi / script: running process ⇒ busy. No "idle" concept.
	return types.InstanceStatusBusy
}

// taskInstance builds an OpencodeInstance record for a tracked task process.
// This is used for every executor type (opencode, pi, script) so the Runners
// tab can render a live "currently executing" row for any in-flight task.
// Fields that only apply to OpenCode (Port, SessionIDs) are populated when
// available and left zero-valued for other executors — the PWA already
// handles those absences.
func (tr *TaskRunner) taskInstance(info *ProcessInfo, hostname string) types.OpencodeInstance {
	var sessionIDs []string
	if info.Task.SessionID != "" {
		sessionIDs = []string{info.Task.SessionID}
	}
	executor := info.Task.ExecutorType
	if executor == "" {
		executor = "opencode" // legacy default
	}
	return types.OpencodeInstance{
		InstanceID: info.Task.InstanceID,
		RunnerID:   tr.runnerID,
		Hostname:   hostname,
		Kind:       types.InstanceKindTask,
		ProjectID:  info.Task.ProjectID,
		TaskID:     info.Task.ID,
		FeatureID:  info.Task.FeatureID,
		Priority:   info.Task.Priority,
		Title:      info.Task.Title,
		Workdir:    info.Task.Workdir,
		Port:       info.Task.OpencodePort,
		PID:        info.Proc.Pid(),
		SessionIDs: sessionIDs,
		Status:     taskInstanceStatus(info),
		Executor:   executor,
		Agent:      info.Task.Agent,
		Model:      info.Task.Model,
		StartedAt:  info.Task.StartedAt.UnixMilli(),
		LastSeen:   time.Now().UnixMilli(),
	}
}

// instanceSnapshot builds the full reconcile list of instances for heartbeat
// payloads. Reports *all* executor types (opencode, pi, script) so the
// Runners tab can show what's currently running regardless of executor.
// Always non-nil so the server can distinguish "zero instances" from
// "runner does not report instances".
func (tr *TaskRunner) instanceSnapshot() []types.OpencodeInstance {
	hostname := runnerHostname()
	out := make([]types.OpencodeInstance, 0)
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.InstanceID == "" {
			continue
		}
		inst := tr.taskInstance(&info, hostname)
		out = append(out, inst)
	}
	if tr.bridgeClient != nil {
		out = append(out, tr.bridgeClient.AdhocInstances(hostname)...)
	}
	return out
}

// reportInstance upserts a single instance record to the Brain API.
// Best-effort: failures are logged and the heartbeat reconcile self-heals.
func (tr *TaskRunner) reportInstance(inst types.OpencodeInstance) {
	reporter, ok := tr.client.(instanceReporterClient)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := reporter.UpsertInstance(ctx, tr.runnerID, inst); err != nil {
		tr.logger.Printf("instance report: upsert %s failed: %v", inst.InstanceID, err)
	}
	// Keep the bridge event pump running for every known instance so
	// control events (permissions, idle) flow even without a browser attached.
	if tr.bridgeClient != nil && inst.Port > 0 {
		tr.bridgeClient.EnsurePump(inst.InstanceID)
	}
}

// removeInstance deletes a single instance record from the Brain API.
// Best-effort: failures are logged and the heartbeat reconcile self-heals.
func (tr *TaskRunner) removeInstance(instanceID string) {
	if instanceID == "" {
		return
	}
	if tr.bridgeClient != nil {
		tr.bridgeClient.StopPump(instanceID)
	}
	reporter, ok := tr.client.(instanceReporterClient)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := reporter.DeleteInstance(ctx, tr.runnerID, instanceID); err != nil {
		tr.logger.Printf("instance report: delete %s failed: %v", instanceID, err)
	}
}
