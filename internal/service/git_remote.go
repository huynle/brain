package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/gitremote"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Marks security preflight failures so the legacy best-effort durable metadata
// sync cannot swallow them and continue with an unauthorized DB update.
type gitRemoteAdmissionError struct{ error }

func (e gitRemoteAdmissionError) Unwrap() error { return e.error }

// Admission describes configured support, not present availability. Offline
// advertisements may admit work; dispatch must independently require a live,
// compatible runner. No registry rows (or a registry error) fails closed.
func validateConfiguredGitRemote(ctx context.Context, store *storage.StorageLayer, remote string) error {
	if remote == "" {
		return nil
	}
	if _, err := gitremote.Parse(remote); err != nil {
		return gitRemoteAdmissionError{fmt.Errorf("%w: %v", api.ErrInvalidInput, err)}
	}
	runners, err := store.ListRunners(ctx)
	if err != nil {
		return gitRemoteAdmissionError{fmt.Errorf("read configured git hosts: %w", err)}
	}
	var hosts []string
	for _, runner := range runners {
		hosts = append(hosts, gitremote.CredentialHosts(runner.Capabilities)...)
	}
	_, err = gitremote.Validate(remote, hosts)
	if err != nil {
		return gitRemoteAdmissionError{fmt.Errorf("%w: %v", api.ErrInvalidInput, err)}
	}
	return nil
}

// Same host/liveness predicate for push, pull, and direct claims. This is
// independent of executor/workdir defaults: a local path is not an exemption.
func runnerGitRemoteError(remote string, runner *types.RunnerInfo) error {
	if remote == "" {
		return nil
	}
	var capabilities []string
	if runner != nil {
		capabilities = runner.Capabilities
	}
	if _, err := gitremote.Validate(remote, gitremote.CredentialHosts(capabilities)); err != nil {
		return err
	}
	if runner == nil || runner.Status != types.RunnerStatusOnline {
		return fmt.Errorf("git remote runner not online")
	}
	if runner.Paused || runner.Draining {
		return fmt.Errorf("git remote runner paused or draining")
	}
	return nil
}

func liveRunnerInfo(row *storage.RunnerRow) *types.RunnerInfo {
	if row == nil {
		return nil
	}
	info := rowToRunnerInfo(row)
	info.Status = computeRunnerStatus(row.LastHeartbeat)
	return info
}

// Metadata updates accept arbitrary keys. Validate the resulting remote before
// either the durable file sync or the DB merge; a type override cannot disguise
// an existing task. Defaults currently contain no git_remote, and local workdir,
// executor and execution-mode defaults never exempt a remote from admission.
func validateMetadataGitRemote(ctx context.Context, store *storage.StorageLayer, row *storage.NoteRow, fields map[string]interface{}) error {
	meta := make(map[string]interface{})
	if row.Metadata != "" {
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
			return fmt.Errorf("parse task metadata: %w", err)
		}
	}
	for k, v := range fields {
		meta[k] = v
	}
	if (row.Type == nil || *row.Type != "task") && meta["type"] != "task" {
		return nil
	}
	value, present := meta["git_remote"]
	if !present {
		return nil
	}
	remote, ok := value.(string)
	if !ok {
		return fmt.Errorf("task git_remote must be a string")
	}
	return validateConfiguredGitRemote(ctx, store, remote)
}
