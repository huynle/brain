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
	rand.Read(b)
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
func taskInstanceStatus(info *ProcessInfo) string {
	if info.IsExited || info.Proc.Exited() {
		return types.InstanceStatusExited
	}
	if info.Task.OpencodePort == 0 {
		return types.InstanceStatusStarting
	}
	if info.Task.IdleSince != "" {
		return types.InstanceStatusIdle
	}
	return types.InstanceStatusBusy
}

// taskInstance builds an OpencodeInstance record for a tracked task process.
func (tr *TaskRunner) taskInstance(info *ProcessInfo, hostname string) types.OpencodeInstance {
	var sessionIDs []string
	if info.Task.SessionID != "" {
		sessionIDs = []string{info.Task.SessionID}
	}
	return types.OpencodeInstance{
		InstanceID: info.Task.InstanceID,
		RunnerID:   tr.runnerID,
		Hostname:   hostname,
		Kind:       types.InstanceKindTask,
		ProjectID:  info.Task.ProjectID,
		TaskID:     info.Task.ID,
		Title:      info.Task.Title,
		Workdir:    info.Task.Workdir,
		Port:       info.Task.OpencodePort,
		PID:        info.Proc.Pid(),
		SessionIDs: sessionIDs,
		Status:     taskInstanceStatus(info),
		Executor:   "opencode",
		StartedAt:  info.Task.StartedAt.UnixMilli(),
		LastSeen:   time.Now().UnixMilli(),
	}
}

// instanceSnapshot builds the full reconcile list of OpenCode instances for
// heartbeat payloads. Always non-nil so the server can distinguish "zero
// instances" from "runner does not report instances".
func (tr *TaskRunner) instanceSnapshot() []types.OpencodeInstance {
	hostname := runnerHostname()
	out := make([]types.OpencodeInstance, 0)
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.ExecutorType != "opencode" || info.Task.InstanceID == "" {
			continue
		}
		inst := tr.taskInstance(&info, hostname)
		out = append(out, inst)
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
}

// removeInstance deletes a single instance record from the Brain API.
// Best-effort: failures are logged and the heartbeat reconcile self-heals.
func (tr *TaskRunner) removeInstance(instanceID string) {
	if instanceID == "" {
		return
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
