package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type BrainClientRow struct {
	ClientID     string            `json:"client_id"`
	Kind         string            `json:"kind"`
	HostID       string            `json:"host_id"`
	Hostname     string            `json:"hostname"`
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	Username     string            `json:"username"`
	HomeDir      string            `json:"home_dir"`
	Labels       map[string]string `json:"labels"`
	Capabilities []string          `json:"capabilities"`
	RegisteredAt int64             `json:"registered_at"`
	LastSeen     int64             `json:"last_seen"`
	Status       string            `json:"status"`
}

type BrainClientWorkspaceRow struct {
	ID               int64  `json:"id"`
	ClientID         string `json:"client_id"`
	HostID           string `json:"host_id"`
	ProjectID        string `json:"project_id"`
	Path             string `json:"path"`
	GitRoot          string `json:"git_root"`
	GitCommonDir     string `json:"git_common_dir"`
	GitWorktreeMain  string `json:"git_worktree_main"`
	GitBranch        string `json:"git_branch"`
	GitRemote        string `json:"git_remote"`
	FolderName       string `json:"folder_name"`
	Confidence       string `json:"confidence"`
	ResolutionSource string `json:"resolution_source"`
	FirstSeen        int64  `json:"first_seen"`
	LastSeen         int64  `json:"last_seen"`
}

func (s *StorageLayer) UpsertBrainClient(ctx context.Context, client *BrainClientRow) error {
	labelsJSON, err := json.Marshal(client.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	capabilitiesJSON, err := json.Marshal(client.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}
	now := time.Now().UnixMilli()
	if client.RegisteredAt == 0 {
		client.RegisteredAt = now
	}
	if client.LastSeen == 0 {
		client.LastSeen = now
	}
	if client.Status == "" {
		client.Status = "online"
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO brain_clients
			(client_id, kind, host_id, hostname, os, arch, username, home_dir,
			 labels, capabilities, registered_at, last_seen, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (client_id) DO UPDATE SET
			kind         = excluded.kind,
			host_id      = excluded.host_id,
			hostname     = excluded.hostname,
			os           = excluded.os,
			arch         = excluded.arch,
			username     = excluded.username,
			home_dir     = excluded.home_dir,
			labels       = excluded.labels,
			capabilities = excluded.capabilities,
			last_seen    = excluded.last_seen,
			status       = excluded.status`,
		client.ClientID, client.Kind, client.HostID, client.Hostname, client.OS, client.Arch,
		client.Username, client.HomeDir, string(labelsJSON), string(capabilitiesJSON),
		client.RegisteredAt, client.LastSeen, client.Status,
	)
	if err != nil {
		return fmt.Errorf("upsert brain client: %w", err)
	}
	return nil
}

func (s *StorageLayer) GetBrainClient(ctx context.Context, clientID string) (*BrainClientRow, error) {
	var r BrainClientRow
	var labelsJSON, capabilitiesJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT client_id, kind, host_id, hostname, os, arch, username, home_dir,
		       labels, capabilities, registered_at, last_seen, status
		FROM brain_clients WHERE client_id = ?`, clientID,
	).Scan(&r.ClientID, &r.Kind, &r.HostID, &r.Hostname, &r.OS, &r.Arch, &r.Username, &r.HomeDir,
		&labelsJSON, &capabilitiesJSON, &r.RegisteredAt, &r.LastSeen, &r.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get brain client: %w", err)
	}
	if err := json.Unmarshal([]byte(labelsJSON), &r.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &r.Capabilities); err != nil {
		return nil, fmt.Errorf("unmarshal capabilities: %w", err)
	}
	return &r, nil
}

func (s *StorageLayer) UpsertBrainClientWorkspace(ctx context.Context, workspace *BrainClientWorkspaceRow) error {
	now := time.Now().UnixMilli()
	if workspace.FirstSeen == 0 {
		workspace.FirstSeen = now
	}
	if workspace.LastSeen == 0 {
		workspace.LastSeen = now
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO brain_client_workspaces
			(client_id, host_id, project_id, path, git_root, git_common_dir, git_worktree_main,
			 git_branch, git_remote, folder_name, confidence, resolution_source, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (client_id, path) DO UPDATE SET
			host_id           = excluded.host_id,
			project_id        = excluded.project_id,
			git_root          = excluded.git_root,
			git_common_dir    = excluded.git_common_dir,
			git_worktree_main = excluded.git_worktree_main,
			git_branch        = excluded.git_branch,
			git_remote        = excluded.git_remote,
			folder_name       = excluded.folder_name,
			confidence        = excluded.confidence,
			resolution_source = excluded.resolution_source,
			last_seen         = excluded.last_seen`,
		workspace.ClientID, workspace.HostID, workspace.ProjectID, workspace.Path, workspace.GitRoot,
		workspace.GitCommonDir, workspace.GitWorktreeMain, workspace.GitBranch, workspace.GitRemote,
		workspace.FolderName, workspace.Confidence, workspace.ResolutionSource,
		workspace.FirstSeen, workspace.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("upsert brain client workspace: %w", err)
	}
	return nil
}

func (s *StorageLayer) ListBrainClientWorkspaces(ctx context.Context, projectID string) ([]BrainClientWorkspaceRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, client_id, host_id, project_id, path, git_root, git_common_dir,
		       git_worktree_main, git_branch, git_remote, folder_name, confidence,
		       resolution_source, first_seen, last_seen
		FROM brain_client_workspaces WHERE project_id = ? ORDER BY last_seen DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list brain client workspaces: %w", err)
	}
	defer rows.Close()

	var result []BrainClientWorkspaceRow
	for rows.Next() {
		var r BrainClientWorkspaceRow
		if err := rows.Scan(&r.ID, &r.ClientID, &r.HostID, &r.ProjectID, &r.Path, &r.GitRoot,
			&r.GitCommonDir, &r.GitWorktreeMain, &r.GitBranch, &r.GitRemote, &r.FolderName,
			&r.Confidence, &r.ResolutionSource, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, fmt.Errorf("scan brain client workspace: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain client workspaces: %w", err)
	}
	if result == nil {
		return []BrainClientWorkspaceRow{}, nil
	}
	return result, nil
}
