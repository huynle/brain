package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func TestChildEnvironmentBoundary(t *testing.T) {
	dir := t.TempDir()
	secret := "standing-secret-fixture"
	for k, v := range map[string]string{"BRAIN_API_TOKEN": secret, "SOURCE_TOKEN": secret, "ACCIDENTAL_COPY": "Bearer " + secret, "BRAIN_TENANT_ID": "spoof", "BRAIN_CLAIM_TOKEN": "spoof", "BENIGN": "keep"} {
		t.Setenv(k, v)
	}
	startup := filepath.Join(dir, "startup")
	startupMarker := filepath.Join(dir, "startup-executed")
	if err := os.WriteFile(startup, []byte("export BRAIN_API_TOKEN=reintroduced\n: > "+shellEnvQuote(startupMarker)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", startup)
	cfg := RunnerConfig{StateDir: dir, BrainAPIURL: "https://trusted.invalid", StandingToken: secret, StandingTokenEnv: "SOURCE_TOKEN", EnvPassthrough: []string{"BRAIN_API_TOKEN", "SOURCE_TOKEN", "ACCIDENTAL_COPY", "BRAIN_TENANT_ID", "BENIGN"}, Script: ScriptConfig{Enabled: true}}
	task := &types.ResolvedTask{ID: "task", Env: map[string]string{"BRAIN_API_URL": "spoof", "BRAIN_RUNNER_ID": "spoof", "BRAIN_API_TOKEN": "spoof", "SOURCE_TOKEN": "override", "COPY": secret, "BAD-KEY": "bad", "SAFE": "literal ' $(false) `false` \" value"}, DirectPrompt: "/usr/bin/env"}
	prompt := filepath.Join(dir, "prompt")
	if err := os.WriteFile(prompt, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	dummy := filepath.Join(dir, "dummy")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\n/usr/bin/env\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Opencode.Bin = dummy
	cfg.Pi.Bin = dummy
	check := func(t *testing.T, data string) {
		t.Helper()
		if _, err := os.Stat(startupMarker); !os.IsNotExist(err) {
			t.Error("shell startup file was executed")
		}
		for _, bad := range []string{secret, "=spoof", "=reintroduced", "SOURCE_TOKEN=", "BRAIN_CLAIM_TOKEN=", "BRAIN_TENANT_ID=", "BAD-KEY="} {
			if strings.Contains(data, bad) {
				t.Errorf("child contains forbidden marker %q", bad)
			}
		}
		if !strings.Contains(data, "BENIGN=keep") {
			t.Error("benign inherited env lost")
		}
	}
	e := NewExecutor(cfg)
	for _, mode := range []string{"script", "direct", "attached", "server", "pi", "legacy-pi", "generated-opencode", "generated-pi", "bridge", "bridge-nil", "hook"} {
		t.Run(mode, func(t *testing.T) {
			var res *SpawnResult
			var err error
			switch mode {
			case "script":
				res, err = e.spawnScript(context.Background(), task, "p", dir, prompt, SpawnOptions{})
			case "direct", "attached":
				port := 0
				if mode == "attached" {
					port = 12345
				}
				res, err = e.spawnHeadlessDirect(dir, "p", task, prompt, SpawnOptions{}, port, "")
			case "server":
				_, _, _, _ = e.startHeadlessServer(dir, "p", task.ID)
				data, readErr := os.ReadFile(filepath.Join(dir, "serve_p_task.log"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				check(t, string(data))
				return
			case "pi":
				res, err = NewPiExecutor(cfg).spawnHeadless(context.Background(), task, "p", dir, prompt, "")
			case "legacy-pi":
				// Legacy RPC owns stdout, so capture the dummy's environment separately.
				out := filepath.Join(dir, "legacy-env")
				legacy := NewExecutor(cfg)
				legacy.CommandFactory = func(_ string, _ ...string) *exec.Cmd {
					return exec.Command("/bin/sh", "-c", "/usr/bin/env > "+out+"; read line")
				}
				res, err = legacy.spawnPi(context.Background(), task, "p", dir, prompt)
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for !res.Proc.Exited() && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				data, readErr := os.ReadFile(out)
				if readErr != nil {
					t.Fatal(readErr)
				}
				check(t, string(data))
				return
			case "generated-opencode", "generated-pi":
				var path string
				if mode == "generated-pi" {
					path, err = NewPiExecutor(cfg).buildRunnerScript(task, "p", dir, prompt, SpawnOptions{})
				} else {
					path, err = e.buildRunnerScript(task, "p", dir, prompt, SpawnOptions{})
				}
				if err != nil {
					t.Fatal(err)
				}
				script, _ := os.ReadFile(path)
				if strings.Contains(string(script), secret) {
					t.Error("standing token written to script")
				}
				cmd := exec.Command(path)
				cmd.Env = append(os.Environ(), "BRAIN_STALE_ID=spoof", "STALE_COPY="+secret)
				data, runErr := cmd.CombinedOutput()
				if runErr != nil {
					t.Fatalf("launcher: %v: %s", runErr, data)
				}
				check(t, string(data))
				return
			case "bridge", "bridge-nil":
				bc := &BridgeClient{}
				if mode == "bridge" {
					bc.runner = &TaskRunner{config: cfg}
				}
				cmd := exec.Command(dummy)
				cmd.Env = bc.execEnv()
				data, runErr := cmd.Output()
				if runErr != nil {
					t.Fatal(runErr)
				}
				check(t, string(data))
				return
			case "hook":
				cmd := exec.Command(dummy)
				cmd.Env = buildHookEnv(types.Event{})
				data, runErr := cmd.Output()
				if runErr != nil {
					t.Fatal(runErr)
				}
				check(t, string(data))
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for !res.Proc.Exited() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !res.Proc.Exited() {
				t.Fatal("child did not exit")
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "output_p_task.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			check(t, string(data))
		})
	}
}

// Integration coverage for the shared policy wired into hook and bridge paths.
func TestChildEnvironmentConfiguredAlias(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRAIN_API_TOKEN", "")
	t.Setenv("CUSTOM_SOURCE", "alias-only-secret")
	t.Setenv("OTHER_COPY", "Bearer alias-only-secret")
	t.Setenv("BENIGN", "still useful")
	cfg := RunnerConfig{StateDir: dir, StandingTokenEnv: "CUSTOM_SOURCE", BrainAPIURL: "https://trusted.invalid"}
	cfg.Control.AllowedWorkdirRoots = []string{dir}
	dummy := filepath.Join(dir, "dump-env")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\n/usr/bin/env\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Opencode.Bin = dummy
	bc := &BridgeClient{runner: &TaskRunner{config: cfg}}
	_, err := bc.spawnAdhoc(&types.SpawnInstanceSpec{Workdir: dir})
	if err == nil || !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("expected dummy serve exit, got %v", err)
	}
	logs, err := filepath.Glob(filepath.Join(dir, "adhoc_*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("ad-hoc child log: %v, %v", logs, err)
	}
	check := func(path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "alias-only-secret") || strings.Contains(string(data), "CUSTOM_SOURCE=") {
			t.Errorf("credential reached child in %s", path)
		}
		if !strings.Contains(string(data), "BENIGN=still useful") {
			t.Errorf("missing benign environment in %s", path)
		}
	}
	check(logs[0])
	hd, err := NewHookDispatcher(t.TempDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	hd.childConfig = cfg
	for _, mode := range []string{"pre", "post", "inline-pre", "inline-post"} {
		t.Run(mode, func(t *testing.T) {
			out := filepath.Join(dir, mode+".env")
			script := filepath.Join(dir, mode+".sh")
			if err := os.WriteFile(script, []byte("#!/bin/bash\n/usr/bin/env > "+shellEnvQuote(out)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "pre":
				err = hd.executePreHook(script, mode, types.Event{})
			case "post":
				err = hd.executePostHook(script, mode, types.Event{})
			case "inline-pre":
				err = hd.executeInlinePreHook(InlineHookConfig{Command: shellEnvQuote(script)}, mode, types.Event{}, time.Second)
			case "inline-post":
				err = hd.executeInlinePostHook(InlineHookConfig{Script: script}, mode, types.Event{}, time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			check(out)
		})
	}
}

func TestChildEnvironmentLiteralExports(t *testing.T) {
	t.Setenv("BRAIN_API_TOKEN", "fixture-standing")
	literal := "quotes ' \" dollar $(touch forbidden) `false`\nnewline"
	task := &types.ResolvedTask{Env: map[string]string{"SAFE": literal, "BRAIN_API_URL": "spoof", "BRAIN_TASK_ID": "spoof", "BRAIN_RUNNER_ID": "spoof", "ENV": "startup", "BASH_ENV": "startup", "9INVALID": "x", "BAD;false": "x", "NUL": "\x00"}}
	cfg := RunnerConfig{BrainAPIURL: "https://trusted.invalid", EnvPassthrough: []string{"BRAIN_API_TOKEN"}}
	cmd := exec.Command("/bin/bash", "--noprofile", "--norc", "-c", CommonBuildEnvExports(task, cfg)+`printf '%s' "$SAFE"`)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != literal {
		t.Fatalf("literal export roundtrip: %q, %v", out, err)
	}
	env := CommonBuildEnvMap(task, cfg)
	if env["BRAIN_API_URL"] != cfg.BrainAPIURL {
		t.Fatal("endpoint overridden")
	}
	for _, key := range []string{"BRAIN_API_TOKEN", "BRAIN_TASK_ID", "BRAIN_RUNNER_ID", "ENV", "BASH_ENV", "9INVALID", "BAD;false", "NUL"} {
		if _, ok := env[key]; ok {
			t.Errorf("reserved/invalid key survived: %s", key)
		}
	}
	if len(defaultEnvPassthrough(nil)) != 0 {
		t.Fatal("default passthrough must not forward Brain credentials")
	}
}
