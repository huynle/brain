package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Server stand-in for the init-reaping test: launches one sealed worker,
// records "server=<pid> worker=<pid>" once the worker is blocked in a brokered
// call, then blocks until killed. Never run directly.
func TestNativeLauncherServerHold(t *testing.T) {
	marker := os.Getenv("BRAIN_NATIVE_HOLD_MARKER")
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if marker == "" || worker == "" {
		t.Skip("init-reaping server stand-in only")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	pinned := filepath.Join(t.TempDir(), "brain-script-worker")
	data, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, data, 0o555); err != nil {
		t.Fatal(err)
	}
	l, err := newWorkerLauncher(launcherConfig{Enabled: true, WorkerPath: pinned, WorkerSHA256: fileSHA256(t, pinned), WallTimeout: launcherMaxWall})
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.run(context.Background(), `return await brain.entries.get("held");`, func(frame Frame, _ func(Frame) error) error {
		if frame.Kind != "call" {
			return nil
		}
		tasks, _ := filepath.Glob("/proc/self/task/*/children")
		child := ""
		for _, task := range tasks {
			if b, err := os.ReadFile(task); err == nil && strings.TrimSpace(string(b)) != "" {
				child = strings.Fields(string(b))[0]
			}
		}
		if err := os.WriteFile(marker, []byte(fmt.Sprintf("server=%d worker=%s\n", os.Getpid(), child)), 0o644); err != nil {
			return err
		}
		select {} // held until this server process is SIGKILLed
	})
	t.Fatalf("server stand-in returned instead of being killed: %v", err)
}

// TestQuickJSInitReaping kills the server process (not the worker) and checks
// what happens to its sealed worker: PDEATHSIG must kill it in every topology;
// only a real init (docker --init / tini as PID 1) reaps it. The control is the
// compiler container whose PID 1 is /bin/sleep, which never reaps.
func TestQuickJSInitReaping(t *testing.T) {
	goarch := os.Getenv("BRAIN_SCRIPT_LINUX_GOARCH")
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
		defer cancel()
		docker := func(stdin []byte, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", host}, args...)...)
			if stdin != nil {
				cmd.Stdin = bytes.NewReader(stdin)
			}
			return cmd.CombinedOutput()
		}
		binary := filepath.Join(t.TempDir(), "launcher.test")
		build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
		if out, e := build.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("cross-build: %w %s", e, out)
		}
		parent, e := os.ReadFile(binary)
		if e != nil {
			return nil, e
		}
		workerBytes, e := docker(nil, "exec", name, "cat", "/tmp/probe")
		if e != nil {
			return nil, fmt.Errorf("read built worker: %w", e)
		}
		install := func(container string) error {
			if out, e := docker(parent, "exec", "-i", container, "/bin/sh", "-c", "cat > /tmp/launcher.test && chmod 755 /tmp/launcher.test"); e != nil {
				return fmt.Errorf("install parent: %w %s", e, out)
			}
			if out, e := docker(workerBytes, "exec", "-i", container, "/bin/sh", "-c", "cat > /tmp/probe && chmod 755 /tmp/probe"); e != nil {
				return fmt.Errorf("install worker: %w %s", e, out)
			}
			return nil
		}
		// scenario starts the server detached, kills only the server, then
		// reports the worker's final observed state: "gone", "Z" or a live state.
		scenario := func(container string) (string, error) {
			markerPath := fmt.Sprintf("/tmp/hold-%d", time.Now().UnixNano())
			serverLog := markerPath + ".log"
			if out, e := docker(nil, "exec", "-d", "--user=65534:65534", container, "/bin/sh", "-c", `exec /usr/bin/env -i BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe BRAIN_NATIVE_HOLD_MARKER="$1" /tmp/launcher.test '-test.run=^TestNativeLauncherServerHold$' -test.v -test.timeout=10m >"$2" 2>&1`, "server", markerPath, serverLog); e != nil {
				return "", fmt.Errorf("start server: %w %s", e, out)
			}
			var server, worker int
			for deadline := time.Now().Add(30 * time.Second); ; {
				// Read as the server's uid: with --cap-drop=ALL, container root
				// has no CAP_DAC_OVERRIDE.
				out, e := docker(nil, "exec", "--user=65534:65534", container, "cat", markerPath)
				if e == nil && strings.Contains(string(out), "worker=") {
					if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "server=%d worker=%d", &server, &worker); err != nil || server <= 1 || worker <= 1 {
						return "", fmt.Errorf("bad marker %q", out)
					}
					break
				}
				if time.Now().After(deadline) {
					log, _ := docker(nil, "exec", "--user=65534:65534", container, "cat", serverLog)
					return "", fmt.Errorf("server never held a worker call; server output:\n%s", log)
				}
				time.Sleep(200 * time.Millisecond)
			}
			if out, e := docker(nil, "exec", "--user=65534:65534", container, "kill", "-9", strconv.Itoa(server)); e != nil {
				return "", fmt.Errorf("kill server: %w %s", e, out)
			}
			state := "?"
			for i := 0; i < 15; i++ {
				time.Sleep(200 * time.Millisecond)
				out, e := docker(nil, "exec", container, "/bin/sh", "-c", fmt.Sprintf("cat /proc/%d/stat 2>/dev/null || echo gone", worker))
				if e != nil {
					return "", fmt.Errorf("inspect worker: %w %s", e, out)
				}
				text := strings.TrimSpace(string(out))
				if text == "gone" {
					state = "gone"
					continue
				}
				// /proc/<pid>/stat: "pid (comm) S ..." — state follows the last ')'.
				if i := strings.LastIndex(text, ")"); i >= 0 && len(text) > i+2 {
					state = text[i+2 : i+3]
				}
			}
			return state, nil
		}

		if e := install(name); e != nil {
			return nil, e
		}
		control, e := scenario(name)
		if e != nil {
			return nil, fmt.Errorf("control: %w", e)
		}
		pid1, _ := docker(nil, "exec", name, "cat", "/proc/1/comm")
		t.Logf("control (PID1=%s, no init): worker state after server SIGKILL = %q", strings.TrimSpace(string(pid1)), control)
		if control != "Z" && control != "gone" {
			return nil, fmt.Errorf("worker survived server death without init (state %q): PDEATHSIG failed", control)
		}

		image := os.Getenv("BRAIN_QUICKJS_PROBE_IMAGE")
		initName := name + "-init"
		if out, e := docker(nil, "create", "--name", initName, "--init", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=512m", "--cpus=1", "--tmpfs=/tmp:rw,exec,nosuid,size=128m,mode=1777", "--entrypoint=/bin/sleep", image, "600"); e != nil {
			return nil, fmt.Errorf("create init container: %w %s", e, out)
		}
		t.Cleanup(func() {
			if out, e := exec.Command("docker", "--host", host, "rm", "-f", initName).CombinedOutput(); e != nil {
				t.Errorf("owned init container cleanup: %v %s", e, out)
			}
		})
		if out, e := docker(nil, "start", initName); e != nil {
			return nil, fmt.Errorf("start init container: %w %s", e, out)
		}
		if e := install(initName); e != nil {
			return nil, e
		}
		pid1, _ = docker(nil, "exec", initName, "cat", "/proc/1/comm")
		withInit, e := scenario(initName)
		if e != nil {
			return nil, fmt.Errorf("init: %w", e)
		}
		t.Logf("init container (PID1=%s): worker state after server SIGKILL = %q", strings.TrimSpace(string(pid1)), withInit)
		if withInit != "gone" {
			return nil, fmt.Errorf("worker not reaped under real init: state %q", withInit)
		}
		if control != "Z" {
			t.Logf("NOTE: control did not retain a zombie (%q); it does not discriminate init reaping in this runtime", control)
		}
		evidence, _ := json.Marshal(map[string]string{"control_pid1": "sleep", "control_state": control, "init_pid1": strings.TrimSpace(string(pid1)), "init_state": withInit})
		t.Logf("init reaping evidence: %s", evidence)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
