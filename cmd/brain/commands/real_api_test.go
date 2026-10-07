package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// startRealAPI runs the actual Brain API server in-process on a free loopback
// port with a throwaway brain dir, so CLI tests see real server semantics
// (mocks hid that /runner/status reports paused=true for ANY paused project).
// TestMain has already pointed HOME at a temp dir.
func startRealAPI(t *testing.T, projects ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- apiserver.RunServer(ctx, apiserver.ServerOptions{
			Host:      "127.0.0.1",
			Port:      port,
			BrainDir:  filepath.Join(t.TempDir(), "brain"),
			LogLevel:  "error",
			LogWriter: io.Discard,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("api server: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("api server did not stop")
		}
	})

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(url + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("api server exited during startup: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("api server not healthy at %s: %v", url, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	client := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: url})
	for _, p := range projects {
		if _, err := client.CreateEntry(context.Background(), types.CreateEntryRequest{
			Type: "task", Title: "seed " + p, Content: "seed", Project: p, Status: "pending",
		}); err != nil {
			t.Fatalf("seed project %s: %v", p, err)
		}
	}
	return url
}
