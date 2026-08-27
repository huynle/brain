package commands

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func attachmentTestConfig(serverURL string) *UnifiedConfig {
	cfg := &UnifiedConfig{}
	cfg.Runner = runner.RunnerConfig{BrainAPIURL: serverURL, APIToken: "test-token", APITimeout: 5000}
	return cfg
}

func TestAttachmentCommandUploadSendsMultipartAndPrintsMetadata(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotProject string
	var gotFilename string
	var gotBody string
	var gotDescription string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/attachments" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotProject = r.FormValue("project_id")
		metadata := map[string]string{}
		if raw := r.FormValue("metadata"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
				t.Fatalf("metadata was not JSON object: %v", err)
			}
		}
		gotDescription = metadata["description"]
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing multipart file: %v", err)
		}
		defer file.Close()
		gotFilename = header.Filename
		body, _ := io.ReadAll(file)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.CreateAttachmentResponse{Attachment: types.Attachment{ID: "att_123", Filename: "hello.txt", ContentType: "text/plain", Size: int64(len(body)), SHA256: shaHex(body)}})
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := &AttachmentCommand{Subcommand: "upload", Path: filePath, Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api", Description: "sample file"}, Out: &out}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotProject != "brain-api" || gotFilename != "hello.txt" || gotBody != "hello world" || gotDescription != "sample file" {
		t.Fatalf("upload request project=%q filename=%q body=%q description=%q", gotProject, gotFilename, gotBody, gotDescription)
	}
	if !strings.Contains(out.String(), "att_123") || !strings.Contains(out.String(), shaHex([]byte("hello world"))) {
		t.Fatalf("output %q does not include attachment metadata", out.String())
	}
}

func TestAttachmentCommandAttachListDetachAndDeleteUseAttachmentEndpoints(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/entries/entry-123/attachments":
			var req types.AttachEntryAttachmentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode attach request: %v", err)
			}
			if req.Attachment.ID != "att_123" || req.Attachment.Role != "source" || req.Attachment.Caption != "design doc" {
				t.Fatalf("attach request = %#v", req)
			}
			_ = json.NewEncoder(w).Encode(types.AttachEntryAttachmentResponse{EntryID: "entry-123", Attachments: []types.AttachmentReference{req.Attachment}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/entries/entry-123/attachments":
			_ = json.NewEncoder(w).Encode(types.AttachEntryAttachmentResponse{EntryID: "entry-123", Attachments: []types.AttachmentReference{{ID: "att_123", Filename: "hello.txt", Role: "source"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments":
			_ = json.NewEncoder(w).Encode(types.ListAttachmentsResponse{Attachments: []types.Attachment{{ID: "att_456", Filename: "project.pdf"}}, Total: 1})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/entries/entry-123/attachments/att_123":
			_ = json.NewEncoder(w).Encode(types.AttachEntryAttachmentResponse{EntryID: "entry-123"})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/attachments/att_456":
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer srv.Close()

	cfg := attachmentTestConfig(srv.URL)
	commands := []*AttachmentCommand{
		{Subcommand: "attach", Entry: "entry-123", AttachmentID: "att_123", Config: cfg, Flags: &AttachmentFlags{Project: "brain-api", Role: "source", Description: "design doc"}, Out: io.Discard},
		{Subcommand: "list", Entry: "entry-123", Config: cfg, Flags: &AttachmentFlags{Project: "brain-api"}, Out: io.Discard},
		{Subcommand: "list", Config: cfg, Flags: &AttachmentFlags{Project: "brain-api"}, Out: io.Discard},
		{Subcommand: "detach", Entry: "entry-123", AttachmentID: "att_123", Config: cfg, Flags: &AttachmentFlags{Project: "brain-api", Role: "source"}, Out: io.Discard},
		{Subcommand: "delete", AttachmentID: "att_456", Config: cfg, Flags: &AttachmentFlags{Project: "brain-api"}, Out: io.Discard},
	}
	for _, cmd := range commands {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s Execute() error = %v", cmd.Subcommand, err)
		}
	}

	want := []string{
		"POST /api/v1/entries/entry-123/attachments?project_id=brain-api",
		"GET /api/v1/entries/entry-123/attachments?project_id=brain-api",
		"GET /api/v1/attachments?project_id=brain-api",
		"DELETE /api/v1/entries/entry-123/attachments/att_123?project_id=brain-api&role=source",
		"DELETE /api/v1/attachments/att_456?project_id=brain-api",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestAttachmentCommandExtractPrintsExtractionSummary(t *testing.T) {
	var gotRequest string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = r.Method + " " + r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.AttachmentExtractionResult{
			Attachment: types.Attachment{ID: "att_123", Filename: "scan.pdf"},
			DerivedText: types.AttachmentDerivedText{
				Status:      types.AttachmentExtractionStatusReady,
				ContentType: "text/markdown",
				Text:        "derived text",
				Metadata:    map[string]string{"provider": "openrouter", "model": "google/gemini"},
			},
			LinkedEntries: []types.AttachmentLinkedEntry{{Path: "projects/brain-api/report/scan.md", Role: "source"}},
		})
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := &AttachmentCommand{Subcommand: "extract", AttachmentID: "att_123", Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api"}, Out: &out}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotRequest != "POST /api/v1/attachments/att_123/extract?project_id=brain-api" {
		t.Fatalf("request = %q, want extract endpoint", gotRequest)
	}
	output := out.String()
	for _, want := range []string{"Status: ready", "Provider: openrouter", "Model: google/gemini", "Content-Type: text/markdown", "Text-Length: 12", "Linked-Entries: 1", "projects/brain-api/report/scan.md (source)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q missing %q", output, want)
		}
	}
}

func TestAttachmentCommandExtractPrintsSkippedReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.AttachmentExtractionResult{
			Attachment:  types.Attachment{ID: "att_skip", Filename: "large.bin"},
			DerivedText: types.AttachmentDerivedText{Status: types.AttachmentExtractionStatusSkipped, Error: "unsupported content type"},
		})
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := &AttachmentCommand{Subcommand: "extract", AttachmentID: "att_skip", Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api"}, Out: &out}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if output := out.String(); !strings.Contains(output, "Status: skipped") || !strings.Contains(output, "Reason: unsupported content type") {
		t.Fatalf("output = %q, want skipped reason", output)
	}
}

func TestAttachmentCommandBackfillSendsRequestAndPrintsPartialFailureSummary(t *testing.T) {
	var gotRequest string
	var gotReq types.AttachmentExtractionBackfillRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = r.Method + " " + r.URL.RequestURI()
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode backfill request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.AttachmentExtractionBackfillResponse{
			Total:      4,
			Candidates: 3,
			Processed:  2,
			Skipped:    1,
			Failed:     1,
			DryRun:     false,
			Attachments: []types.AttachmentExtractionBackfillItem{
				{AttachmentID: "att_ready", Filename: "ready.pdf", Status: string(types.AttachmentExtractionStatusReady)},
				{AttachmentID: "att_fail", Filename: "bad.png", Status: string(types.AttachmentExtractionStatusFailed), Error: "model unavailable"},
				{AttachmentID: "att_skip", Filename: "done.pdf", Status: string(types.AttachmentExtractionStatusReady), Skipped: true, Reason: "already ready"},
			},
		})
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := &AttachmentCommand{
		Subcommand: "backfill",
		Config:     attachmentTestConfig(srv.URL),
		Flags:      &AttachmentFlags{Project: "brain-api"},
		Out:        &out,
	}
	setAttachmentFlagForTest(t, cmd.Flags, "Force", true)
	setAttachmentFlagForTest(t, cmd.Flags, "BatchSize", 10)
	setAttachmentFlagForTest(t, cmd.Flags, "RateLimitDelayMs", 25)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotRequest != "POST /api/v1/attachments/backfill/extraction?project_id=brain-api" {
		t.Fatalf("request = %q, want backfill endpoint", gotRequest)
	}
	if !gotReq.Force || gotReq.DryRun || gotReq.BatchSize != 10 || gotReq.RateLimitDelayMs != 25 {
		t.Fatalf("request body = %#v", gotReq)
	}
	output := out.String()
	for _, want := range []string{
		"Attachment extraction backfill complete",
		"Total: 4",
		"Candidates: 3",
		"Processed: 2",
		"Skipped: 1",
		"Failed: 1",
		"Partial failures:",
		"att_fail bad.png: model unavailable",
		"Skipped attachments:",
		"att_skip done.pdf: already ready",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q missing %q", output, want)
		}
	}
}

func TestAttachmentCommandBackfillDryRunOutputIsExplicit(t *testing.T) {
	var gotReq types.AttachmentExtractionBackfillRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode backfill request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.AttachmentExtractionBackfillResponse{
			Total:      2,
			Candidates: 2,
			DryRun:     true,
			Attachments: []types.AttachmentExtractionBackfillItem{
				{AttachmentID: "att_one", Filename: "one.pdf", Status: string(types.AttachmentExtractionStatusPending)},
				{AttachmentID: "att_two", Filename: "two.png", Status: string(types.AttachmentExtractionStatusPending)},
			},
		})
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := &AttachmentCommand{Subcommand: "backfill", Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api"}, Out: &out}
	setAttachmentFlagForTest(t, cmd.Flags, "DryRun", true)
	setAttachmentFlagForTest(t, cmd.Flags, "BatchSize", 2)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !gotReq.DryRun || gotReq.BatchSize != 2 {
		t.Fatalf("request body = %#v", gotReq)
	}
	output := out.String()
	for _, want := range []string{"DRY RUN", "would extract", "att_one one.pdf", "att_two two.png", "Processed: 0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q missing %q", output, want)
		}
	}
}

func TestAttachmentCommandBackfillRequiresProject(t *testing.T) {
	cmd := &AttachmentCommand{Subcommand: "backfill", Flags: &AttachmentFlags{}, Out: io.Discard}
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--project is required") {
		t.Fatalf("Execute() error = %v, want --project required", err)
	}
}

func TestAttachmentCommandTimeoutsAllowLongRunningExtractionWorkflows(t *testing.T) {
	tests := []struct {
		name        string
		subcommand  string
		wantAtLeast time.Duration
	}{
		{name: "extract", subcommand: "extract", wantAtLeast: time.Minute},
		{name: "backfill", subcommand: "backfill", wantAtLeast: 30 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &AttachmentCommand{Subcommand: tt.subcommand}
			if got := cmd.timeout(); got < tt.wantAtLeast {
				t.Fatalf("timeout() = %s, want at least %s", got, tt.wantAtLeast)
			}
		})
	}
}

func TestAttachmentCommandTimeoutsKeepFastTimeoutForNormalOperations(t *testing.T) {
	for _, subcommand := range []string{"upload", "attach", "list", "download", "detach", "delete", ""} {
		t.Run(subcommand, func(t *testing.T) {
			cmd := &AttachmentCommand{Subcommand: subcommand}
			if got := cmd.timeout(); got != 30*time.Second {
				t.Fatalf("timeout() = %s, want 30s", got)
			}
		})
	}
}

func TestAttachmentCommandDownloadWritesExactBytesAndVerifiesSHA256(t *testing.T) {
	payload := []byte{0, 1, 2, 3, 255, 'b', 'r', 'a', 'i', 'n'}
	sum := shaHex(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments/att_bytes":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(types.Attachment{ID: "att_bytes", Filename: "bytes.bin", Size: int64(len(payload)), SHA256: sum})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments/att_bytes/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(payload)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	cmd := &AttachmentCommand{Subcommand: "download", AttachmentID: "att_bytes", Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api", Output: dest}, Out: io.Discard}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded bytes = %v, want %v", got, payload)
	}
}

func TestAttachmentCommandDownloadRejectsSHA256Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			_, _ = w.Write([]byte("actual"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.Attachment{ID: "att_bad", Filename: "bad.txt", SHA256: shaHex([]byte("expected"))})
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "bad.txt")
	cmd := &AttachmentCommand{Subcommand: "download", AttachmentID: "att_bad", Config: attachmentTestConfig(srv.URL), Flags: &AttachmentFlags{Project: "brain-api", Output: dest}, Out: io.Discard}
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Execute() error = %v, want sha256 mismatch", err)
	}
}

func shaHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func setAttachmentFlagForTest(t *testing.T, flags *AttachmentFlags, name string, value any) {
	t.Helper()
	field := reflect.ValueOf(flags).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("AttachmentFlags missing field %s", name)
	}
	field.Set(reflect.ValueOf(value))
}
