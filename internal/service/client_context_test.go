package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"

	_ "github.com/glebarez/go-sqlite"
)

func newTestClientContextService(t *testing.T) (*ClientContextServiceImpl, *storage.StorageLayer) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return NewClientContextService(store), store
}

func insertDreamNote(t *testing.T, store *storage.StorageLayer, projectID, shortID, title, content, modified string) {
	t.Helper()
	ctx := context.Background()

	metadata, err := json.Marshal(map[string]interface{}{})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	typ := "dream"
	status := "active"
	created := "2026-01-01T00:00:00Z"
	path := "projects/" + projectID + "/dream/" + shortID + ".md"
	note := &storage.NoteRow{
		Path:       path,
		ShortID:    shortID,
		Title:      title,
		RawContent: &content,
		Metadata:   string(metadata),
		Type:       &typ,
		Status:     &status,
		ProjectID:  &projectID,
		Created:    &created,
		Modified:   &modified,
	}
	if _, err := store.InsertNote(ctx, note); err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}
}

func TestClientContextResolveRegistersWorkspaceAndReturnsLatestDream(t *testing.T) {
	svc, store := newTestClientContextService(t)
	insertDreamNote(t, store, "brain", "old", "Old Dream", "old context", "2026-01-01T00:00:00Z")
	insertDreamNote(t, store, "brain", "new", "Current Dream", "current context", "2026-01-02T00:00:00Z")

	resp, err := svc.Resolve(context.Background(), types.ResolveClientContextRequest{
		Client: types.BrainClientInfo{
			ClientID: "client-1",
			Kind:     "opencode",
			HostID:   "host-1",
		},
		Workspace: types.WorkspaceObservation{
			Path:            "/Users/me/code/brain",
			GitRoot:         "/Users/me/code/brain",
			GitWorktreeMain: "/Users/me/code/brain",
			GitBranch:       "dev",
			GitRemote:       "git@github.com:huynle/brain-api.git",
			FolderName:      "brain",
		},
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resp.ProjectID != "brain" {
		t.Fatalf("ProjectID = %q, want brain", resp.ProjectID)
	}
	if resp.Confidence != "high" {
		t.Fatalf("Confidence = %q, want high", resp.Confidence)
	}
	if resp.Dream == nil {
		t.Fatal("Dream = nil, want latest dream")
	}
	if resp.Dream.ID != "new" || resp.Dream.Content != "current context" {
		t.Fatalf("Dream = %+v, want latest dream content", resp.Dream)
	}

	storedClient, err := store.GetBrainClient(context.Background(), "client-1")
	if err != nil {
		t.Fatalf("GetBrainClient failed: %v", err)
	}
	if storedClient == nil || storedClient.HostID != "host-1" || storedClient.Kind != "opencode" {
		t.Fatalf("stored client = %+v, want registered opencode client", storedClient)
	}

	workspaces, err := store.ListBrainClientWorkspaces(context.Background(), "brain")
	if err != nil {
		t.Fatalf("ListBrainClientWorkspaces failed: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspace count = %d, want 1", len(workspaces))
	}
	if workspaces[0].ClientID != "client-1" || workspaces[0].ProjectID != "brain" || workspaces[0].GitBranch != "dev" {
		t.Fatalf("workspace = %+v, want registered workspace for project", workspaces[0])
	}
}
