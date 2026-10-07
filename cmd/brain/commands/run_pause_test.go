package commands

import (
	"bytes"
	"context"
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
	if !strings.Contains(ue.Message, "brain runner stop") {
		t.Errorf("message should also cover runners started with 'brain runner start': %q", ue.Message)
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

// Real server: with ONE of two projects paused, /runner/status reports
// paused=true. resume-all must not claim every project is paused.
func TestRunResumeAll_RealServer_OneOfTwoPaused(t *testing.T) {
	url := startRealAPI(t, "alpha", "beta")
	pause, _ := pauseCmd(url, "pause", []string{"alpha"}, RunnerFlags{}, "", false)
	if err := pause.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd, out := pauseCmd(url, "resume-all", nil, RunnerFlags{}, "n\n", true)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	o := out.String()
	if strings.Contains(strings.ToLower(o), "all projects are paused") {
		t.Errorf("one of two projects paused, but output claims all are:\n%s", o)
	}
	if !strings.Contains(o, "Currently paused projects (1)") || !strings.Contains(o, "alpha") || strings.Contains(o, "  beta") {
		t.Errorf("should list exactly alpha:\n%s", o)
	}
}

func TestRunResumeAll_RealServer_AllPaused(t *testing.T) {
	url := startRealAPI(t, "alpha", "beta")
	pauseAll, _ := pauseCmd(url, "pause-all", nil, RunnerFlags{Yes: true}, "", false)
	if err := pauseAll.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd, out := pauseCmd(url, "resume-all", nil, RunnerFlags{Yes: true}, "", false)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "All 2 projects are paused") {
		t.Errorf("every project was paused; output should say so:\n%s", out.String())
	}
	status, err := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: url}).GetRunnerStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.PausedProjects) != 0 {
		t.Errorf("resume-all left projects paused: %v", status.PausedProjects)
	}
}

// pause-all/resume-all take no project: a stray positional must fail as a
// usage error before any request, even with --yes.
func TestRunPauseAllResumeAll_RejectPositionals(t *testing.T) {
	url := startRealAPI(t, "demo", "other")
	client := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: url})
	for _, sub := range []string{"pause-all", "resume-all"} {
		cmd, _ := pauseCmd(url, sub, []string{"demo"}, RunnerFlags{Yes: true}, "", false)
		err := cmd.Execute()
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Fatalf("%s demo: err = %v, want usage error", sub, err)
		}
		want := "brain run " + strings.TrimSuffix(sub, "-all") + " <project>"
		if !strings.Contains(ue.Message, want) {
			t.Errorf("%s demo: message %q should point at %q", sub, ue.Message, want)
		}
		status, err := client.GetRunnerStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(status.PausedProjects) != 0 {
			t.Fatalf("%s demo changed server state: paused %v", sub, status.PausedProjects)
		}
	}
	api, fakeURL := newPauseAPI(t)
	for _, sub := range []string{"pause-all", "resume-all"} {
		cmd, _ := pauseCmd(fakeURL, sub, []string{"demo"}, RunnerFlags{Yes: true}, "", false)
		_ = cmd.Execute()
	}
	if len(api.requests) != 0 {
		t.Errorf("rejected commands must make no request: %v", api.requests)
	}
}

// Per-project commands take exactly their documented positionals; extras
// (`run pause demo other`) are usage errors with no request.
func TestRunProjectCommands_RejectExtraPositionals(t *testing.T) {
	api, url := newPauseAPI(t)
	for _, tc := range []struct {
		sub  string
		args []string
	}{
		{"pause", []string{"demo", "other"}},
		{"resume", []string{"demo", "other"}},
		{"features", []string{"demo", "other"}},
		{"logs", []string{"demo", "task1", "extra"}},
	} {
		cmd, _ := pauseCmd(url, tc.sub, tc.args, RunnerFlags{}, "", false)
		err := cmd.Execute()
		var ue *UsageError
		if !errors.As(err, &ue) || !strings.Contains(ue.Message, "unexpected argument") || !strings.Contains(ue.Message, tc.args[len(tc.args)-1]) {
			t.Errorf("brain run %s %v: err = %v, want usage error naming the extra argument", tc.sub, tc.args, err)
		}
	}
	if len(api.requests) != 0 {
		t.Errorf("rejected commands must make no request: %v", api.requests)
	}
}
