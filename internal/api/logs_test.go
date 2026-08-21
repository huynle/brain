package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// appendNumberedLines seeds the buffer for my-project/task-123 with n lines
// named line-0 … line-(n-1), so a test can tell head from tail.
func appendNumberedLines(n int) func(lb *logbuffer.Buffer) {
	return func(lb *logbuffer.Buffer) {
		lines := make([]types.LogLine, n)
		for i := range lines {
			lines[i] = types.LogLine{
				Timestamp: "2025-01-01T00:00:00Z",
				Level:     "info",
				Content:   fmt.Sprintf("line-%d", i),
			}
		}
		lb.Append("my-project", "task-123", lines)
	}
}

func newLogTestRouter(lb *logbuffer.Buffer, hub *realtime.Hub) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithLogBuffer(lb), WithHub(hub))
	r := chi.NewRouter()
	r.Route("/tasks/{projectId}/{taskId}/logs", func(r chi.Router) {
		r.Post("/", h.HandleIngestLogs)
		r.Get("/", h.HandleGetLogs)
	})
	return r
}

// jsonBody and decodeJSON are defined in entries_test.go (same package)

func TestHandleIngestLogs(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "valid ingest",
			body: types.LogIngestRequest{
				RunnerID: "runner-1",
				Lines: []types.LogLine{
					{Timestamp: "2025-01-01T00:00:00Z", Level: "info", Content: "hello"},
					{Timestamp: "2025-01-01T00:00:01Z", Level: "error", Content: "world"},
				},
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogIngestResponse](t, resp)
				if result.Accepted != 2 {
					t.Errorf("expected accepted=2, got %d", result.Accepted)
				}
			},
		},
		{
			name:       "invalid JSON",
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing runnerId",
			body: types.LogIngestRequest{
				Lines: []types.LogLine{
					{Timestamp: "2025-01-01T00:00:00Z", Level: "info", Content: "hello"},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty lines",
			body: types.LogIngestRequest{
				RunnerID: "runner-1",
				Lines:    []types.LogLine{},
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := realtime.NewHub()
			lb := logbuffer.New(logbuffer.DefaultMaxLines)
			router := newLogTestRouter(lb, hub)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/tasks/my-project/task-123/logs/", "application/json", body)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

func TestHandleGetLogs(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(lb *logbuffer.Buffer)
		query      string
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:       "empty logs",
			setup:      func(lb *logbuffer.Buffer) {},
			query:      "",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 0 {
					t.Errorf("expected total=0, got %d", result.Total)
				}
				if len(result.Lines) != 0 {
					t.Errorf("expected 0 lines, got %d", len(result.Lines))
				}
			},
		},
		{
			name: "returns stored logs",
			setup: func(lb *logbuffer.Buffer) {
				lb.Append("my-project", "task-123", []types.LogLine{
					{Timestamp: "2025-01-01T00:00:00Z", Level: "info", Content: "line 1"},
					{Timestamp: "2025-01-01T00:00:01Z", Level: "info", Content: "line 2"},
					{Timestamp: "2025-01-01T00:00:02Z", Level: "error", Content: "line 3"},
				})
			},
			query:      "",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 3 {
					t.Errorf("expected total=3, got %d", result.Total)
				}
				if len(result.Lines) != 3 {
					t.Errorf("expected 3 lines, got %d", len(result.Lines))
				}
				if result.Lines[0].Content != "line 1" {
					t.Errorf("expected first line 'line 1', got '%s'", result.Lines[0].Content)
				}
			},
		},
		{
			name: "pagination with offset and limit",
			setup: func(lb *logbuffer.Buffer) {
				lines := make([]types.LogLine, 10)
				for i := range lines {
					lines[i] = types.LogLine{
						Timestamp: "2025-01-01T00:00:00Z",
						Level:     "info",
						Content:   "line",
					}
				}
				lb.Append("my-project", "task-123", lines)
			},
			query:      "?offset=3&limit=4",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 10 {
					t.Errorf("expected total=10, got %d", result.Total)
				}
				if len(result.Lines) != 4 {
					t.Errorf("expected 4 lines, got %d", len(result.Lines))
				}
				if result.Offset != 3 {
					t.Errorf("expected offset=3, got %d", result.Offset)
				}
				if result.Limit != 4 {
					t.Errorf("expected limit=4, got %d", result.Limit)
				}
			},
		},
		{
			// Regression: a limit with no offset used to return the OLDEST
			// `limit` lines, so a long-running task showed a stale prefix and
			// the newest output existed only in the live SSE tail.
			name:       "limit without offset returns the newest lines",
			setup:      appendNumberedLines(250),
			query:      "?limit=10",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 250 {
					t.Errorf("expected total=250, got %d", result.Total)
				}
				if len(result.Lines) != 10 {
					t.Fatalf("expected 10 lines, got %d", len(result.Lines))
				}
				if result.Offset != 240 {
					t.Errorf("expected offset=240 (tail start), got %d", result.Offset)
				}
				if result.Limit != 10 {
					t.Errorf("expected limit=10, got %d", result.Limit)
				}
				// Oldest→newest ordering preserved within the tail window.
				for i, line := range result.Lines {
					want := fmt.Sprintf("line-%d", 240+i)
					if line.Content != want {
						t.Fatalf("line %d: expected %q, got %q", i, want, line.Content)
					}
				}
			},
		},
		{
			name:       "no limit and no offset returns the newest default page",
			setup:      appendNumberedLines(250),
			query:      "",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if len(result.Lines) != 100 {
					t.Fatalf("expected 100 lines (default limit), got %d", len(result.Lines))
				}
				if result.Offset != 150 {
					t.Errorf("expected offset=150, got %d", result.Offset)
				}
				if result.Lines[0].Content != "line-150" {
					t.Errorf("expected first='line-150', got %q", result.Lines[0].Content)
				}
				if result.Lines[99].Content != "line-249" {
					t.Errorf("expected last='line-249', got %q", result.Lines[99].Content)
				}
			},
		},
		{
			// Explicit offset keeps head-indexed pagination — offset 0 is the
			// oldest retained line, not the tail.
			name:       "explicit offset=0 pages from the oldest retained line",
			setup:      appendNumberedLines(250),
			query:      "?offset=0&limit=10",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 250 {
					t.Errorf("expected total=250, got %d", result.Total)
				}
				if result.Offset != 0 {
					t.Errorf("expected offset=0, got %d", result.Offset)
				}
				if len(result.Lines) != 10 {
					t.Fatalf("expected 10 lines, got %d", len(result.Lines))
				}
				if result.Lines[0].Content != "line-0" {
					t.Errorf("expected first='line-0', got %q", result.Lines[0].Content)
				}
				if result.Lines[9].Content != "line-9" {
					t.Errorf("expected last='line-9', got %q", result.Lines[9].Content)
				}
			},
		},
		{
			name:       "explicit mid-buffer offset returns that window",
			setup:      appendNumberedLines(250),
			query:      "?offset=100&limit=5",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Offset != 100 {
					t.Errorf("expected offset=100, got %d", result.Offset)
				}
				if len(result.Lines) != 5 {
					t.Fatalf("expected 5 lines, got %d", len(result.Lines))
				}
				if result.Lines[0].Content != "line-100" {
					t.Errorf("expected first='line-100', got %q", result.Lines[0].Content)
				}
			},
		},
		{
			name:       "explicit offset beyond total returns empty window",
			setup:      appendNumberedLines(10),
			query:      "?offset=500&limit=10",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				result := decodeJSON[types.LogQueryResponse](t, resp)
				if result.Total != 10 {
					t.Errorf("expected total=10, got %d", result.Total)
				}
				if len(result.Lines) != 0 {
					t.Errorf("expected 0 lines, got %d", len(result.Lines))
				}
				if result.Offset != 500 {
					t.Errorf("expected offset=500 echoed back, got %d", result.Offset)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := realtime.NewHub()
			lb := logbuffer.New(logbuffer.DefaultMaxLines)
			tt.setup(lb)
			router := newLogTestRouter(lb, hub)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/my-project/task-123/logs/" + tt.query)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

func TestHandleIngestLogs_EmptyContentLines_Rejected(t *testing.T) {
	tests := []struct {
		name         string
		body         string // raw JSON to simulate wrong field names
		wantStatus   int
		wantAccepted int // -1 means don't check
		wantStored   int // expected lines in buffer after request
	}{
		{
			name:       "all lines have wrong field name (empty content)",
			body:       `{"runnerId":"runner-1","lines":[{"timestamp":"2025-01-01T00:00:00Z","level":"info","text":"hello"}]}`,
			wantStatus: http.StatusBadRequest,
			wantStored: 0,
		},
		{
			name:       "all lines have explicitly empty content",
			body:       `{"runnerId":"runner-1","lines":[{"timestamp":"2025-01-01T00:00:00Z","level":"info","content":""}]}`,
			wantStatus: http.StatusBadRequest,
			wantStored: 0,
		},
		{
			name:       "all lines have whitespace-only content",
			body:       `{"runnerId":"runner-1","lines":[{"timestamp":"2025-01-01T00:00:00Z","level":"info","content":"   "}]}`,
			wantStatus: http.StatusBadRequest,
			wantStored: 0,
		},
		{
			name:         "valid content accepted normally",
			body:         `{"runnerId":"runner-1","lines":[{"timestamp":"2025-01-01T00:00:00Z","level":"info","content":"hello world"}]}`,
			wantStatus:   http.StatusOK,
			wantAccepted: 1,
			wantStored:   1,
		},
		{
			name:         "mix of valid and empty-content lines stores only valid",
			body:         `{"runnerId":"runner-1","lines":[{"timestamp":"2025-01-01T00:00:00Z","level":"info","content":"good line"},{"timestamp":"2025-01-01T00:00:01Z","level":"info","text":"wrong field"},{"timestamp":"2025-01-01T00:00:02Z","level":"info","content":"another good"}]}`,
			wantStatus:   http.StatusOK,
			wantAccepted: 2,
			wantStored:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := realtime.NewHub()
			lb := logbuffer.New(logbuffer.DefaultMaxLines)
			router := newLogTestRouter(lb, hub)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/tasks/my-project/task-456/logs/", "application/json", bytes.NewBufferString(tt.body))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantAccepted > 0 {
				result := decodeJSON[types.LogIngestResponse](t, resp)
				if result.Accepted != tt.wantAccepted {
					t.Errorf("accepted = %d, want %d", result.Accepted, tt.wantAccepted)
				}
			}

			// Verify what actually got stored in the buffer
			stored, total := lb.Query("my-project", "task-456", 0, 100)
			if total != tt.wantStored {
				t.Errorf("stored lines = %d, want %d", total, tt.wantStored)
			}
			for i, line := range stored {
				if line.Content == "" {
					t.Errorf("stored line[%d] has empty content", i)
				}
			}
		})
	}
}

func TestIngestLogs_SSEBroadcast(t *testing.T) {
	hub := realtime.NewHub()
	lb := logbuffer.New(logbuffer.DefaultMaxLines)
	router := newLogTestRouter(lb, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Subscribe to project SSE events
	ch, unsub := hub.Subscribe("my-project")
	defer unsub()

	// Ingest logs
	body := jsonBody(t, types.LogIngestRequest{
		RunnerID: "runner-1",
		Lines: []types.LogLine{
			{Timestamp: "2025-01-01T00:00:00Z", Level: "info", Content: "test broadcast"},
		},
	})

	resp, err := http.Post(srv.URL+"/tasks/my-project/task-123/logs/", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read SSE event
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case msg := <-ch:
		if msg.Event != "runner_log" {
			t.Errorf("expected event='runner_log', got '%s'", msg.Event)
		}
		data, ok := msg.Data.(types.SSERunnerLogData)
		if !ok {
			t.Fatalf("expected SSERunnerLogData, got %T", msg.Data)
		}
		if data.TaskID != "task-123" {
			t.Errorf("expected taskId='task-123', got '%s'", data.TaskID)
		}
		if data.RunnerID != "runner-1" {
			t.Errorf("expected runnerId='runner-1', got '%s'", data.RunnerID)
		}
		if len(data.Lines) != 1 {
			t.Errorf("expected 1 line, got %d", len(data.Lines))
		}
		if data.Lines[0].Content != "test broadcast" {
			t.Errorf("expected content='test broadcast', got '%s'", data.Lines[0].Content)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestIngestAndRetrieve_RoundTrip(t *testing.T) {
	hub := realtime.NewHub()
	lb := logbuffer.New(logbuffer.DefaultMaxLines)
	router := newLogTestRouter(lb, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Ingest
	body := jsonBody(t, types.LogIngestRequest{
		RunnerID: "runner-1",
		Lines: []types.LogLine{
			{Timestamp: "2025-01-01T00:00:00Z", Level: "info", Content: "first"},
			{Timestamp: "2025-01-01T00:00:01Z", Level: "warn", Content: "second"},
		},
	})

	resp, err := http.Post(srv.URL+"/tasks/proj-1/task-abc/logs/", "application/json", body)
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	resp.Body.Close()

	// Retrieve
	resp, err = http.Get(srv.URL + "/tasks/proj-1/task-abc/logs/")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer resp.Body.Close()

	result := decodeJSON[types.LogQueryResponse](t, resp)
	if result.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Total)
	}
	if result.Lines[0].Content != "first" {
		t.Errorf("expected 'first', got '%s'", result.Lines[0].Content)
	}
	if result.Lines[1].Content != "second" {
		t.Errorf("expected 'second', got '%s'", result.Lines[1].Content)
	}
}
