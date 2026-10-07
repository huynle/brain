package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func infoCmd(t *testing.T, sub string, args []string, flags RunnerFlags, h http.HandlerFunc) (*RunCommand, *bytes.Buffer, *[]string) {
	t.Helper()
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Method+" "+r.URL.RequestURI())
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	cmd, out := pauseCmd(srv.URL, sub, args, flags, "", false)
	return cmd, out, &reqs
}

func TestRunFeatures_PrintsTable(t *testing.T) {
	cmd, out, reqs := infoCmd(t, "features", []string{"proj"}, RunnerFlags{}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(types.FeatureListResponse{Features: []types.Feature{
			{FeatureID: "auth", Ready: true, Tasks: []types.ResolvedTask{
				{ID: "t1", Status: "completed"}, {ID: "t2", Status: "pending", Classification: "ready"},
			}},
			{FeatureID: "billing", Ready: false, UnresolvedFeatureDeps: []string{"payments"}, Tasks: []types.ResolvedTask{
				{ID: "t3", Status: "pending", Classification: "blocked"},
			}},
		}})
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*reqs, ","); got != "GET /api/v1/tasks/proj/features" {
		t.Errorf("requests = %s", got)
	}
	o := out.String()
	for _, want := range []string{"FEATURE", "TASKS", "COMPLETED", "READY", "BLOCKED", "STARTABLE", "auth", "billing", "payments"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(o), " "), "auth 2 1 1 0 yes billing 1 0 0 1 no") {
		t.Errorf("auth row wrong:\n%s", o)
	}
}

func TestRunFeatures_RequiresProject(t *testing.T) {
	cmd, _, reqs := infoCmd(t, "features", nil, RunnerFlags{}, func(http.ResponseWriter, *http.Request) {})
	var ue *UsageError
	if err := cmd.Execute(); !errors.As(err, &ue) {
		t.Fatalf("err = %v, want usage error", err)
	}
	if len(*reqs) != 0 {
		t.Errorf("unexpected requests %v", *reqs)
	}
}

func TestRunLogs_PrintsLines(t *testing.T) {
	cmd, out, reqs := infoCmd(t, "logs", []string{"proj", "task1"}, RunnerFlags{Limit: 2}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(types.LogQueryResponse{
			Lines: []types.LogLine{
				{Timestamp: "2026-10-07T10:00:00Z", Level: "info", Content: "started"},
				{Timestamp: "2026-10-07T10:00:01Z", Level: "error", Content: "boom"},
			},
			Total: 5, Offset: 3, Limit: 2,
		})
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*reqs, ","); got != "GET /api/v1/tasks/proj/task1/logs?limit=2" {
		t.Errorf("requests = %s", got)
	}
	o := out.String()
	flat := strings.Join(strings.Fields(o), " ")
	for _, want := range []string{"2026-10-07T10:00:00Z info started", "2026-10-07T10:00:01Z error boom", "2 of 5"} {
		if !strings.Contains(flat, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
}

func TestRunLogs_FollowIsOneShotWithNotice(t *testing.T) {
	for _, flags := range []RunnerFlags{{Follow: true}, {Foreground: true}} {
		cmd, out, reqs := infoCmd(t, "logs", []string{"proj", "task1"}, flags, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(types.LogQueryResponse{Lines: []types.LogLine{}})
		})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if len(*reqs) != 1 {
			t.Errorf("follow must be one-shot, got %v", *reqs)
		}
		if !strings.Contains(out.String(), "does not stream") || !strings.Contains(out.String(), "No log lines") {
			t.Errorf("output:\n%s", out.String())
		}
	}
}

func TestRunLogs_RequiresProjectAndTask(t *testing.T) {
	for _, args := range [][]string{nil, {"proj"}} {
		cmd, _, reqs := infoCmd(t, "logs", args, RunnerFlags{}, func(http.ResponseWriter, *http.Request) {})
		var ue *UsageError
		if err := cmd.Execute(); !errors.As(err, &ue) || !strings.Contains(ue.Message, "brain run logs <project> <taskId>") {
			t.Fatalf("args %v: err = %v, want usage error", args, err)
		}
		if len(*reqs) != 0 {
			t.Errorf("unexpected requests %v", *reqs)
		}
	}
}

func TestRunConfig_PrintsTaskDefaults(t *testing.T) {
	yes := true
	cmd, out, reqs := infoCmd(t, "config", nil, RunnerFlags{}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(runner.TaskDefaultsResponse{Agent: "tdd-dev", ExecutionMode: "worktree", CompleteOnIdle: &yes})
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*reqs, ","); got != "GET /api/v1/config/task-defaults" {
		t.Errorf("requests = %s", got)
	}
	o := out.String()
	for _, want := range []string{"KEY", "VALUE", "agent", "tdd-dev", "execution_mode", "worktree", "complete_on_idle", "true", "model"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
}

func TestRunConfig_ErrorsWhenUnavailable(t *testing.T) {
	cmd, _, _ := infoCmd(t, "config", nil, RunnerFlags{}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "task defaults") {
		t.Fatalf("err = %v", err)
	}
}
