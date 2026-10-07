package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// pauseAPI is a fake Brain API that records every request. It is the only
// server these tests talk to.
type pauseAPI struct {
	mu       sync.Mutex
	requests []string
	status   types.RunnerStatusResponse
	projects []string
}

func newPauseAPI(t *testing.T) (*pauseAPI, string) {
	t.Helper()
	api := &pauseAPI{projects: []string{"alpha", "beta", "gamma"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.requests = append(api.requests, r.Method+" "+r.URL.Path)
		api.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
			_ = json.NewEncoder(w).Encode(types.ProjectListResponse{Projects: api.projects})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/runner/status":
			_ = json.NewEncoder(w).Encode(api.status)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/runner/"):
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return api, srv.URL
}

func (a *pauseAPI) posts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, r := range a.requests {
		if strings.HasPrefix(r, "POST ") {
			out = append(out, r)
		}
	}
	return out
}

func pauseCmd(url, sub string, args []string, flags RunnerFlags, stdin string, tty bool) (*RunCommand, *bytes.Buffer) {
	var out bytes.Buffer
	project := "all"
	if len(args) > 0 {
		project = args[0]
	}
	return &RunCommand{
		Subcommand:      sub,
		Project:         project,
		Args:            args,
		Config:          &UnifiedConfig{Runner: runner.RunnerConfig{BrainAPIURL: url}},
		Flags:           &flags,
		In:              strings.NewReader(stdin),
		Out:             &out,
		StdinIsTerminal: func() bool { return tty },
	}, &out
}

func TestRunStop_IsRenamedAndCallsNothing(t *testing.T) {
	api, url := newPauseAPI(t)
	cmd, _ := pauseCmd(url, "stop", nil, RunnerFlags{}, "", true)
	err := cmd.Execute()
	var ue *UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want a usage error", err)
	}
	want := "`brain run stop` was renamed to `brain run pause-all` (it pauses all projects server-wide; it never stopped the local runner). To stop a local runner, stop its process."
	if !strings.Contains(ue.Message, want) {
		t.Errorf("message = %q, want it to contain %q", ue.Message, want)
	}
	if len(api.requests) != 0 {
		t.Errorf("renamed stop must not touch the API: %v", api.requests)
	}
}

func TestRunPauseAll_RefusesWithoutTTYOrYes(t *testing.T) {
	api, url := newPauseAPI(t)
	cmd, _ := pauseCmd(url, "pause-all", nil, RunnerFlags{}, "y\n", false)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want a refusal naming --yes", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("a refused pause-all must not contact the server at all: %v", api.requests)
	}
}

func TestRunPauseAll_YesSkipsPromptAndReportsCount(t *testing.T) {
	api, url := newPauseAPI(t)
	cmd, out := pauseCmd(url, "pause-all", nil, RunnerFlags{Yes: true}, "", false)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if p := api.posts(); len(p) != 1 || p[0] != "POST /api/v1/tasks/runner/pause" {
		t.Errorf("posts = %v, want one POST /api/v1/tasks/runner/pause", p)
	}
	if !strings.Contains(out.String(), "3 projects") {
		t.Errorf("output should say how many projects are paused:\n%s", out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Errorf("--yes should skip the prompt:\n%s", out.String())
	}
}

func TestRunPauseAll_InteractivePrompt(t *testing.T) {
	for _, tc := range []struct {
		answer    string
		wantPause bool
	}{{"y\n", true}, {"yes\n", true}, {"n\n", false}, {"\n", false}, {"", false}} {
		api, url := newPauseAPI(t)
		cmd, out := pauseCmd(url, "pause-all", nil, RunnerFlags{}, tc.answer, true)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("answer %q: %v", tc.answer, err)
		}
		if !strings.Contains(out.String(), "[y/N]") || !strings.Contains(out.String(), "3 projects") {
			t.Errorf("answer %q: prompt missing count or [y/N]:\n%s", tc.answer, out.String())
		}
		if got := len(api.posts()) == 1; got != tc.wantPause {
			t.Errorf("answer %q: paused = %v, want %v", tc.answer, got, tc.wantPause)
		}
	}
}

func TestRunResumeAll_ListsPausedProjectsAndWarns(t *testing.T) {
	api, url := newPauseAPI(t)
	api.status = types.RunnerStatusResponse{PausedProjects: []string{"alpha", "beta"}}
	cmd, out := pauseCmd(url, "resume-all", nil, RunnerFlags{}, "y\n", true)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	o := out.String()
	for _, want := range []string{"alpha", "beta", "individually", "[y/N]"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	if strings.Index(o, "alpha") > strings.Index(o, "[y/N]") {
		t.Errorf("paused projects must be listed before the prompt:\n%s", o)
	}
	if p := api.posts(); len(p) != 1 || p[0] != "POST /api/v1/tasks/runner/resume" {
		t.Errorf("posts = %v, want one POST /api/v1/tasks/runner/resume", p)
	}
}

func TestRunResumeAll_RefusesWithoutTTYOrYes(t *testing.T) {
	api, url := newPauseAPI(t)
	api.status = types.RunnerStatusResponse{Paused: true, PausedProjects: []string{"alpha"}}
	cmd, _ := pauseCmd(url, "resume-all", nil, RunnerFlags{}, "y\n", false)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want a refusal naming --yes", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("a refused resume-all must not contact the server at all: %v", api.requests)
	}

	cmd, _ = pauseCmd(url, "resume-all", nil, RunnerFlags{Yes: true}, "", false)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if p := api.posts(); len(p) != 1 {
		t.Errorf("--yes should resume: %v", p)
	}
}

func TestRunResumeAll_NothingPausedDoesNothing(t *testing.T) {
	api, url := newPauseAPI(t)
	cmd, out := pauseCmd(url, "resume-all", nil, RunnerFlags{Yes: true}, "", false)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if p := api.posts(); len(p) != 0 {
		t.Errorf("nothing was paused, yet resume posted: %v", p)
	}
	if !strings.Contains(out.String(), "No projects are paused") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestRunPauseResumeProject(t *testing.T) {
	api, url := newPauseAPI(t)
	cmd, out := pauseCmd(url, "pause", []string{"alpha"}, RunnerFlags{}, "", false)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd2, out2 := pauseCmd(url, "resume", []string{"alpha"}, RunnerFlags{}, "", false)
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /api/v1/tasks/runner/pause/alpha", "POST /api/v1/tasks/runner/resume/alpha"}
	if p := api.posts(); strings.Join(p, ",") != strings.Join(want, ",") {
		t.Errorf("posts = %v, want %v", p, want)
	}
	if !strings.Contains(out.String(), "alpha") || !strings.Contains(out2.String(), "alpha") {
		t.Errorf("outputs should name the project: %q / %q", out.String(), out2.String())
	}
}

func TestRunPauseResumeProject_RequiresAProject(t *testing.T) {
	api, url := newPauseAPI(t)
	for _, sub := range []string{"pause", "resume"} {
		for _, args := range [][]string{nil, {"all"}} {
			cmd, _ := pauseCmd(url, sub, args, RunnerFlags{Yes: true}, "", false)
			err := cmd.Execute()
			var ue *UsageError
			if !errors.As(err, &ue) {
				t.Fatalf("brain run %s %v: err = %v, want usage error", sub, args, err)
			}
			if !strings.Contains(ue.Message, sub+"-all") {
				t.Errorf("brain run %s %v should point at %s-all: %q", sub, args, sub, ue.Message)
			}
		}
	}
	if len(api.requests) != 0 {
		t.Errorf("no request expected: %v", api.requests)
	}
}
