package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

type ClientContextServiceImpl struct {
	storage *storage.StorageLayer
}

func NewClientContextService(store *storage.StorageLayer) *ClientContextServiceImpl {
	return &ClientContextServiceImpl{storage: store}
}

func (s *ClientContextServiceImpl) Resolve(ctx context.Context, req types.ResolveClientContextRequest) (*types.ResolveClientContextResponse, error) {
	if s == nil || s.storage == nil {
		return nil, fmt.Errorf("client context service is not configured")
	}
	clientID := strings.TrimSpace(req.Client.ClientID)
	hostID := strings.TrimSpace(req.Client.HostID)
	if clientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if hostID == "" {
		return nil, fmt.Errorf("host_id is required")
	}

	if err := s.storage.UpsertBrainClient(ctx, &storage.BrainClientRow{
		ClientID:     clientID,
		Kind:         strings.TrimSpace(req.Client.Kind),
		HostID:       hostID,
		Hostname:     strings.TrimSpace(req.Client.Hostname),
		OS:           strings.TrimSpace(req.Client.OS),
		Arch:         strings.TrimSpace(req.Client.Arch),
		Username:     strings.TrimSpace(req.Client.Username),
		HomeDir:      strings.TrimSpace(req.Client.HomeDir),
		Labels:       req.Client.Labels,
		Capabilities: req.Client.Capabilities,
	}); err != nil {
		return nil, fmt.Errorf("register brain client: %w", err)
	}

	projectID, confidence, source := resolveProjectFromObservation(req.Workspace)
	if projectID == "" {
		projectID = "unknown"
		confidence = "low"
		source = "fallback"
	}

	if err := s.storage.UpsertBrainClientWorkspace(ctx, &storage.BrainClientWorkspaceRow{
		ClientID:         clientID,
		HostID:           hostID,
		ProjectID:        projectID,
		Path:             strings.TrimSpace(req.Workspace.Path),
		GitRoot:          strings.TrimSpace(req.Workspace.GitRoot),
		GitCommonDir:     strings.TrimSpace(req.Workspace.GitCommonDir),
		GitWorktreeMain:  strings.TrimSpace(req.Workspace.GitWorktreeMain),
		GitBranch:        strings.TrimSpace(req.Workspace.GitBranch),
		GitRemote:        strings.TrimSpace(req.Workspace.GitRemote),
		FolderName:       strings.TrimSpace(req.Workspace.FolderName),
		Confidence:       confidence,
		ResolutionSource: source,
	}); err != nil {
		return nil, fmt.Errorf("register brain client workspace: %w", err)
	}

	dream, err := s.latestDream(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return &types.ResolveClientContextResponse{
		ProjectID:  projectID,
		Confidence: confidence,
		Source:     source,
		Dream:      dream,
	}, nil
}

func resolveProjectFromObservation(obs types.WorkspaceObservation) (projectID, confidence, source string) {
	if folder := strings.TrimSpace(obs.FolderName); folder != "" {
		return sanitizeProjectID(folder), "high", "folder_name"
	}
	for _, path := range []string{obs.GitWorktreeMain, obs.GitRoot, obs.Path} {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			return sanitizeProjectID(filepath.Base(trimmed)), "high", "path_basename"
		}
	}
	return "", "", ""
}

func sanitizeProjectID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *ClientContextServiceImpl) latestDream(ctx context.Context, projectID string) (*types.DreamContext, error) {
	rows, err := s.storage.ListNotes(ctx, &storage.ListOptions{
		Type:      "dream",
		ProjectID: projectID,
		SortBy:    "modified",
		SortOrder: "desc",
		Limit:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("list project dreams: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	content := ""
	if row.RawContent != nil && *row.RawContent != "" {
		content = *row.RawContent
	} else if row.Body != nil {
		content = *row.Body
	}
	return &types.DreamContext{
		ID:      row.ShortID,
		Title:   row.Title,
		Path:    row.Path,
		Content: content,
	}, nil
}
