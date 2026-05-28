package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// TestRunServer_BasicStartup tests that RunServer can start and stop gracefully.
func TestRunServer_BasicStartup(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	opts := ServerOptions{
		Host:     "localhost",
		Port:     0, // Let OS assign a port
		BrainDir: brainDir,
		LogLevel: "error", // Quiet during tests
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServer(ctx, opts)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for server to stop
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Fatalf("RunServer failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop within timeout")
	}
}

// TestRunServer_ContextCancellation tests that the server respects context cancellation.
func TestRunServer_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	opts := ServerOptions{
		Host:     "localhost",
		Port:     0,
		BrainDir: brainDir,
		LogLevel: "error",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start server
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServer(ctx, opts)
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Cancel immediately
	cancel()

	// Server should stop within shutdown timeout
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(12 * time.Second): // 10s shutdown timeout + 2s buffer
		t.Fatal("server did not respect context cancellation")
	}
}

// TestRunServer_InvalidBrainDir tests error handling for invalid brain directory.
func TestRunServer_InvalidBrainDir(t *testing.T) {
	opts := ServerOptions{
		Host:     "localhost",
		Port:     0,
		BrainDir: "/nonexistent/path/that/does/not/exist",
		LogLevel: "error",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := RunServer(ctx, opts)
	if err == nil {
		t.Fatal("expected error for invalid brain dir, got nil")
	}
}

// TestRunServer_PortAlreadyInUse tests handling when port is already bound.
func TestRunServer_PortAlreadyInUse(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	// Start a dummy server to occupy a port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start dummy listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	opts := ServerOptions{
		Host:     "localhost",
		Port:     port, // Use the occupied port
		BrainDir: brainDir,
		LogLevel: "error",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = RunServer(ctx, opts)
	if err == nil {
		t.Fatal("expected error for port already in use, got nil")
	}
}

func TestNormalizeAttachmentConfigDefaultsStorageAndMaxSize(t *testing.T) {
	brainDir := filepath.Join(t.TempDir(), "brain")

	got := normalizeAttachmentConfig(brainDir, config.AttachmentConfig{})

	if got.StorageRoot != filepath.Join(brainDir, "attachments") {
		t.Fatalf("StorageRoot = %q, want %q", got.StorageRoot, filepath.Join(brainDir, "attachments"))
	}
	if got.MaxUploadSizeBytes != defaultAttachmentMaxUploadSizeBytes {
		t.Fatalf("MaxUploadSizeBytes = %d, want %d", got.MaxUploadSizeBytes, defaultAttachmentMaxUploadSizeBytes)
	}
}

func TestNormalizeAttachmentConfigPreservesExplicitValues(t *testing.T) {
	explicitRoot := filepath.Join(t.TempDir(), "custom-blobs")

	got := normalizeAttachmentConfig("/brain", config.AttachmentConfig{StorageRoot: explicitRoot, MaxUploadSizeBytes: 42})

	if got.StorageRoot != explicitRoot {
		t.Fatalf("StorageRoot = %q, want %q", got.StorageRoot, explicitRoot)
	}
	if got.MaxUploadSizeBytes != 42 {
		t.Fatalf("MaxUploadSizeBytes = %d, want 42", got.MaxUploadSizeBytes)
	}
}

func TestAttachmentExtractionDisabledOrMissingKeySkipsThroughProductionWiring(t *testing.T) {
	tests := []struct {
		name        string
		extraction  config.AttachmentExtractionConfig
		wantErrText string
	}{
		{
			name: "disabled",
			extraction: config.AttachmentExtractionConfig{
				Enabled: false,
			},
			wantErrText: "disabled",
		},
		{
			name: "missing api key",
			extraction: config.AttachmentExtractionConfig{
				Enabled:   true,
				APIKeyEnv: "BRAIN_TEST_MISSING_OPENROUTER_API_KEY",
			},
			wantErrText: "api key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.extraction.APIKeyEnv != "" {
				t.Setenv(tt.extraction.APIKeyEnv, "")
			}
			serverURL, dbPath, storageRoot, cleanup := newProductionWiredAttachmentTestServer(t, tt.extraction)
			defer cleanup()

			created := uploadAttachmentThroughServer(t, serverURL, "test-project", "scan.png", "image/png", []byte("not actually an image, but stored as one"))
			removeAttachmentBlobForTest(t, storageRoot, created.Attachment.SHA256)

			resp, statusCode, body := extractAttachmentThroughServer(t, serverURL, "test-project", created.Attachment.ID)
			if statusCode != http.StatusOK {
				t.Fatalf("extract status = %d, body = %s", statusCode, body)
			}
			if resp.DerivedText.Status != types.AttachmentExtractionStatusSkipped {
				t.Fatalf("derived status = %q, want skipped; response = %#v", resp.DerivedText.Status, resp.DerivedText)
			}
			if !strings.Contains(strings.ToLower(resp.DerivedText.Error), tt.wantErrText) {
				t.Fatalf("derived error = %q, want to contain %q", resp.DerivedText.Error, tt.wantErrText)
			}
			if strings.Contains(strings.ToLower(resp.DerivedText.Error), "blob") {
				t.Fatalf("disabled/missing-key extraction read the blob before skipping: %q", resp.DerivedText.Error)
			}

			assertPersistedSkippedDerivedText(t, dbPath, created.Attachment.ID, tt.wantErrText)
		})
	}
}

func newProductionWiredAttachmentTestServer(t *testing.T, extraction config.AttachmentExtractionConfig) (string, string, string, func()) {
	t.Helper()
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	storageRoot := filepath.Join(tempDir, "attachments")
	ctx, cancel := context.WithCancel(context.Background())
	handler, dbPath, cleanup, err := buildHTTPHandler(ctx, ServerOptions{
		Host:     "127.0.0.1",
		Port:     0,
		BrainDir: brainDir,
		LogLevel: "error",
		Attachments: config.AttachmentConfig{
			StorageRoot:        storageRoot,
			MaxUploadSizeBytes: 1024 * 1024,
		},
		AttachmentExtraction: extraction,
	})
	if err != nil {
		cancel()
		t.Fatalf("buildHTTPHandler failed: %v", err)
	}
	server := httptest.NewServer(handler)
	return server.URL, dbPath, storageRoot, func() {
		server.Close()
		cancel()
		cleanup()
	}
}

func uploadAttachmentThroughServer(t *testing.T, serverURL, projectID, filename, contentType string, content []byte) types.CreateAttachmentResponse {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("project_id", projectID); err != nil {
		t.Fatalf("WriteField project_id failed: %v", err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart failed: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart writer close failed: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/v1/attachments", &body)
	if err != nil {
		t.Fatalf("NewRequest upload failed: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer res.Body.Close()
	var created types.CreateAttachmentResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode upload response failed: %v", err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, response = %#v", res.StatusCode, created)
	}
	return created
}

func extractAttachmentThroughServer(t *testing.T, serverURL, projectID, attachmentID string) (types.AttachmentExtractionResult, int, string) {
	t.Helper()
	res, err := http.Post(serverURL+"/api/v1/attachments/"+attachmentID+"/extract?project_id="+projectID, "application/json", nil)
	if err != nil {
		t.Fatalf("extract request failed: %v", err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(res.Body); err != nil {
		t.Fatalf("read extract response failed: %v", err)
	}
	var result types.AttachmentExtractionResult
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw.Bytes(), &result); err != nil {
			t.Fatalf("decode extract response failed: %v", err)
		}
	}
	return result, res.StatusCode, raw.String()
}

func removeAttachmentBlobForTest(t *testing.T, storageRoot, digest string) {
	t.Helper()
	if len(digest) < 4 {
		t.Fatalf("attachment digest too short: %q", digest)
	}
	blobPath := filepath.Join(storageRoot, digest[:2], digest[2:4], digest)
	if err := os.Remove(blobPath); err != nil {
		t.Fatalf("remove blob fixture %s failed: %v", blobPath, err)
	}
}

func assertPersistedSkippedDerivedText(t *testing.T, dbPath, attachmentID, wantErrText string) {
	t.Helper()
	rowID, err := strconv.ParseInt(attachmentID, 10, 64)
	if err != nil {
		t.Fatalf("parse attachment ID %q failed: %v", attachmentID, err)
	}
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("storage.New(%q) failed: %v", dbPath, err)
	}
	defer store.Close()
	derived, err := store.GetAttachmentDerived(context.Background(), rowID, "text")
	if err != nil {
		t.Fatalf("GetAttachmentDerived failed: %v", err)
	}
	if derived == nil {
		t.Fatal("expected persisted derived text row, got nil")
	}
	if derived.Status != types.AttachmentExtractionStatusSkipped {
		t.Fatalf("persisted status = %q, want skipped", derived.Status)
	}
	if !strings.Contains(strings.ToLower(derived.Error), wantErrText) {
		t.Fatalf("persisted error = %q, want to contain %q", derived.Error, wantErrText)
	}
}
