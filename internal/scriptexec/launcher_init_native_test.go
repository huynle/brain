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

// Server stand-in for the reaping tests: launches one sealed worker through
// the launcher (which sets PDEATHSIG=SIGKILL in the child before exec), waits
// until it is blocked in a brokered call, then reopens the worker's stdin pipe
// for writing and hands it to a separate holder process that outlives the
// server. The worker therefore can never observe stdin EOF when the server dies:
// only PDEATHSIG (or an init/cgroup kill) can end it. Records
// "server=<pid> worker=<pid> holder=<pid>" and blocks until killed.
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
		if child == "" {
			return fmt.Errorf("worker child not found")
		}
		stdin, err := os.OpenFile("/proc/"+child+"/fd/0", os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("reopen worker stdin: %w", err)
		}
		holder := exec.Command("/bin/sleep", "600")
		holder.ExtraFiles = []*os.File{stdin}
		if err := holder.Start(); err != nil {
			return fmt.Errorf("start stdin holder: %w", err)
		}
		_ = stdin.Close()
		if err := os.WriteFile(marker, []byte(fmt.Sprintf("server=%d worker=%s holder=%d\n", os.Getpid(), child, holder.Process.Pid)), 0o644); err != nil {
			return err
		}
		select {} // held until this server process is SIGKILLed
	})
	t.Fatalf("server stand-in returned instead of being killed: %v", err)
}

// TestQuickJSInitReaping kills the server process (not the worker) while a
// holder keeps the worker's stdin open, so stdin EOF cannot end the worker. The
// launcher-installed PDEATHSIG must kill it with SIGKILL in every topology:
// with no init (PID 1 /bin/sleep never reaps) it must remain a zombie whose
// recorded termination signal is 9; with a real init (docker --init) it must
// be gone. Removing the launcher's PDEATHSIG leaves the worker alive.
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
		scenario := func(container string) (reapResult, error) {
			markerPath := fmt.Sprintf("/tmp/hold-%d", time.Now().UnixNano())
			serverLog := markerPath + ".log"
			if out, e := docker(nil, "exec", "-d", "--user=65534:65534", container, "/bin/sh", "-c", `exec /usr/bin/env -i BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe BRAIN_NATIVE_HOLD_MARKER="$1" /tmp/launcher.test '-test.run=^TestNativeLauncherServerHold$' -test.v -test.timeout=10m >"$2" 2>&1`, "server", markerPath, serverLog); e != nil {
				return reapResult{}, fmt.Errorf("start server: %w %s", e, out)
			}
			var server, worker, holder int
			for deadline := time.Now().Add(30 * time.Second); ; {
				// Read as the server's uid: with --cap-drop=ALL, container root
				// has no CAP_DAC_OVERRIDE.
				out, e := docker(nil, "exec", "--user=65534:65534", container, "cat", markerPath)
				if e == nil && strings.Contains(string(out), "worker=") {
					if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "server=%d worker=%d holder=%d", &server, &worker, &holder); err != nil || server <= 1 || worker <= 1 || holder <= 1 {
						return reapResult{}, fmt.Errorf("bad marker %q", out)
					}
					break
				}
				if time.Now().After(deadline) {
					log, _ := docker(nil, "exec", "--user=65534:65534", container, "cat", serverLog)
					return reapResult{}, fmt.Errorf("server never held a worker call; server output:\n%s", log)
				}
				time.Sleep(200 * time.Millisecond)
			}
			if out, e := docker(nil, "exec", "--user=65534:65534", container, "kill", "-9", strconv.Itoa(server)); e != nil {
				return reapResult{}, fmt.Errorf("kill server: %w %s", e, out)
			}
			// procState returns "gone", or the /proc/<pid>/stat state letter plus
			// the wait-status exit_code (field 52; meaningful for zombies).
			procState := func(pid int) (string, int, error) {
				// Same uid as the worker: /proc/<pid>/stat zeroes exit_code for
				// readers without ptrace-read access (cap-dropped root included).
				out, e := docker(nil, "exec", "--user=65534:65534", container, "/bin/sh", "-c", fmt.Sprintf("cat /proc/%d/stat 2>/dev/null || echo gone", pid))
				if e != nil {
					return "", 0, fmt.Errorf("inspect %d: %w %s", pid, e, out)
				}
				text := strings.TrimSpace(string(out))
				if text == "gone" {
					return "gone", 0, nil
				}
				i := strings.LastIndex(text, ")")
				if i < 0 {
					return "", 0, fmt.Errorf("bad stat %q", text)
				}
				fields := strings.Fields(text[i+1:])
				code := -1
				if len(fields) > 49 {
					code, _ = strconv.Atoi(fields[49])
				}
				return fields[0], code, nil
			}
			result := reapResult{state: "?"}
			for i := 0; i < 15; i++ {
				time.Sleep(200 * time.Millisecond)
				state, code, e := procState(worker)
				if e != nil {
					return result, e
				}
				result.state, result.signal = state, -1
				if state == "Z" && code >= 0 {
					result.signal = code & 0x7f
				}
			}
			holderState, _, e := procState(holder)
			if e != nil {
				return result, e
			}
			result.holderAlive = holderState != "gone" && holderState != "Z" && holderState != "X"
			_, _ = docker(nil, "exec", "--user=65534:65534", container, "kill", "-9", strconv.Itoa(holder))
			return result, nil
		}

		if e := install(name); e != nil {
			return nil, e
		}
		control, e := scenario(name)
		if e != nil {
			return nil, fmt.Errorf("control: %w", e)
		}
		pid1, _ := docker(nil, "exec", name, "cat", "/proc/1/comm")
		t.Logf("control (PID1=%s, no init): worker after server SIGKILL = %+v", strings.TrimSpace(string(pid1)), control)
		if !control.holderAlive {
			return nil, fmt.Errorf("stdin holder died; PDEATHSIG not isolated from stdin EOF: %+v", control)
		}
		// Exactly a zombie killed by SIGKILL: PDEATHSIG fired (stdin was held
		// open, so the worker could not exit by itself) and nothing reaped it.
		if control.state != "Z" || control.signal != 9 {
			return nil, fmt.Errorf("without init want zombie killed by signal 9 (launcher PDEATHSIG), got %+v", control)
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
		t.Logf("init container (PID1=%s): worker after server SIGKILL = %+v", strings.TrimSpace(string(pid1)), withInit)
		if !withInit.holderAlive || withInit.state != "gone" {
			return nil, fmt.Errorf("under real init want worker killed (stdin held) and reaped, got %+v", withInit)
		}
		evidence, _ := json.Marshal(map[string]any{"control_pid1": "sleep", "control_state": control.state, "control_signal": control.signal, "init_pid1": strings.TrimSpace(string(pid1)), "init_state": withInit.state, "stdin_held": true})
		t.Logf("init reaping evidence: %s", evidence)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type reapResult struct {
	state       string // "gone" or /proc state letter
	signal      int    // termination signal of a zombie, else -1
	holderAlive bool   // stdin writer still held when the worker was observed
}
