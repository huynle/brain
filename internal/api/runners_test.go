package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

type mockRunnersService struct {
	registerFunc            func(ctx context.Context, req types.RegisterRunnerRequest) (*types.RunnerInfo, error)
	heartbeatFunc           func(ctx context.Context, req types.HeartbeatRequest) error
	listFunc                func(ctx context.Context) (*types.RunnerListResponse, error)
	markStaleAndReleaseFunc func(ctx context.Context) ([]string, error)
}

func (m *mockRunnersService) Register(ctx context.Context, req types.RegisterRunnerRequest) (*types.RunnerInfo, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, req)
	}
	return nil, fmt.Errorf("registerFunc not set")
}

func (m *mockRunnersService) Heartbeat(ctx context.Context, req types.HeartbeatRequest) error {
	if m.heartbeatFunc != nil {
		return m.heartbeatFunc(ctx, req)
	}
	return fmt.Errorf("heartbeatFunc not set")
}

func (m *mockRunnersService) List(ctx context.Context) (*types.RunnerListResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, fmt.Errorf("listFunc not set")
}

func (m *mockRunnersService) MarkStaleAndRelease(ctx context.Context) ([]string, error) {
	if m.markStaleAndReleaseFunc != nil {
		return m.markStaleAndReleaseFunc(ctx)
	}
	return nil, fmt.Errorf("markStaleAndReleaseFunc not set")
}

func newRunnerShutdownTestServer(runners RunnersService, hub *realtime.Hub) *httptest.Server {
	h := NewHandler(&mockBrainService{}, WithRunnersService(runners), WithHub(hub))
	return httptest.NewServer(NewRouter(testConfig(), WithHandler(h)))
}

func onlineRunnerList(runnerID string) *types.RunnerListResponse {
	return &types.RunnerListResponse{
		Runners: []types.RunnerInfo{{RunnerID: runnerID, Status: "online"}},
		Total:   1,
	}
}

func postRunnerShutdown(t *testing.T, baseURL, runnerID string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/runners/"+runnerID+"/shutdown", body)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestHandleShutdownRunnerPublishesShutdownCommand(t *testing.T) {
	hub := realtime.NewHub()
	ch, unsub := hub.Subscribe("runner-1")
	defer unsub()

	runners := &mockRunnersService{
		listFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return onlineRunnerList("runner-1"), nil
		},
	}
	srv := newRunnerShutdownTestServer(runners, hub)
	defer srv.Close()

	resp := postRunnerShutdown(t, srv.URL, "runner-1", strings.NewReader(`{"reason":"maintenance"}`))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case msg := <-ch:
		if msg.Event != "shutdown" {
			t.Fatalf("event = %q, want shutdown", msg.Event)
		}
		payload, ok := msg.Data.(map[string]string)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]string", msg.Data)
		}
		if payload["reason"] != "maintenance" {
			t.Fatalf("reason = %q, want maintenance", payload["reason"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown command")
	}
}

func TestHandleShutdownRunnerDefaultReason(t *testing.T) {
	hub := realtime.NewHub()
	ch, unsub := hub.Subscribe("runner-1")
	defer unsub()

	runners := &mockRunnersService{
		listFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return onlineRunnerList("runner-1"), nil
		},
	}
	srv := newRunnerShutdownTestServer(runners, hub)
	defer srv.Close()

	resp := postRunnerShutdown(t, srv.URL, "runner-1", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case msg := <-ch:
		payload, ok := msg.Data.(map[string]string)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]string", msg.Data)
		}
		if payload["reason"] != "remote shutdown requested" {
			t.Fatalf("reason = %q, want default reason", payload["reason"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown command")
	}
}

func TestHandleShutdownRunnerMissingRunner(t *testing.T) {
	srv := newRunnerShutdownTestServer(&mockRunnersService{
		listFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return &types.RunnerListResponse{}, nil
		},
	}, realtime.NewHub())
	defer srv.Close()

	resp := postRunnerShutdown(t, srv.URL, "missing", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandleShutdownRunnerNonOnlineRunner(t *testing.T) {
	srv := newRunnerShutdownTestServer(&mockRunnersService{
		listFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return &types.RunnerListResponse{
				Runners: []types.RunnerInfo{{RunnerID: "runner-1", Status: "lost"}},
				Total:   1,
			}, nil
		},
	}, realtime.NewHub())
	defer srv.Close()

	resp := postRunnerShutdown(t, srv.URL, "runner-1", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

func TestHandleShutdownRunnerMissingHub(t *testing.T) {
	srv := newRunnerShutdownTestServer(&mockRunnersService{
		listFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return onlineRunnerList("runner-1"), nil
		},
	}, nil)
	defer srv.Close()

	resp := postRunnerShutdown(t, srv.URL, "runner-1", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	body := decodeJSON[types.ErrorResponse](t, resp)
	if !strings.Contains(body.Message, "realtime hub") {
		t.Fatalf("message = %q, want clear realtime hub error", body.Message)
	}
}
