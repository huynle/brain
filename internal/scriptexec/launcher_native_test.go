package scriptexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Runs inside the opt-in Linux fixture, directly parenting the real sealed
// worker through the production launcher (no Docker CLI mistaken for the PID).
func TestNativeLauncher(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	// The fixture binary is built writable by its owner in tmpfs; production
	// installs it read-only. Pin a read-only copy to model that.
	pinned := filepath.Join(t.TempDir(), "brain-script-worker")
	data, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, data, 0o555); err != nil {
		t.Fatal(err)
	}
	digest := fileSHA256(t, pinned)
	// The operator pin comes from an INDEPENDENT relocated rebuild when the
	// host wrapper supplies it; the shipped artifact must match it byte-for-byte.
	if pin := os.Getenv("BRAIN_NATIVE_WORKER_PIN"); pin != "" {
		if pin != digest {
			t.Fatalf("shipped worker %s does not match reproducible rebuild pin %s", digest, pin)
		}
		t.Logf("launch pin from independent relocated rebuild: %s", pin)
	}
	config := func(path, sum string, wall time.Duration) launcherConfig {
		return launcherConfig{Enabled: true, WorkerPath: path, WorkerSHA256: sum, WallTimeout: wall}
	}
	twoCalls := `const a=await brain.entries.get("first");const b=await brain.entries.get("second");return a.value+b.value;`

	t.Run("exchange", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, 3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		session, _ := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
		defer session.Retire()
		calls := 0
		var result json.RawMessage
		start := time.Now()
		report, err := l.run(context.Background(), twoCalls, func(frame Frame, reply func(Frame) error) error {
			message, e := session.Accept(frame)
			if e != nil {
				return e
			}
			if message.Done {
				result = message.Result
				return nil
			}
			calls++
			out, e := session.Reply(json.RawMessage(`{"value":21}`))
			if e != nil {
				return e
			}
			return reply(out)
		})
		if err != nil || calls != 2 || string(result) != "42" {
			t.Fatalf("err=%v calls=%d result=%s", err, calls, result)
		}
		if !report.attested || !report.waited || report.attestation.seccompMode != 2 || report.attestation.asHard > launcherAddressSpaceCeiling {
			t.Fatalf("incomplete launch report: %+v", report)
		}
		t.Logf("pinned launcher exchange: calls=%d result=%s attestation=%+v in %v", calls, result, report.attestation, time.Since(start))
	})

	t.Run("pin mismatch refused before start", func(t *testing.T) {
		wrong := strings.Repeat("0", 64)
		l, err := newWorkerLauncher(config(pinned, wrong, time.Second))
		if err != nil {
			t.Fatal(err)
		}
		report, err := l.run(context.Background(), "1", func(Frame, func(Frame) error) error { return errors.New("must not run") })
		if !errors.Is(err, errWorkerPin) || report.started {
			t.Fatalf("err=%v report=%+v", err, report)
		}
	})

	t.Run("world-writable binary refused", func(t *testing.T) {
		loose := filepath.Join(t.TempDir(), "loose-worker")
		if err := os.WriteFile(loose, data, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(loose, 0o777); err != nil {
			t.Fatal(err)
		}
		l, err := newWorkerLauncher(config(loose, digest, time.Second))
		if err != nil {
			t.Fatal(err)
		}
		report, err := l.run(context.Background(), "1", func(Frame, func(Frame) error) error { return errors.New("must not run") })
		if !errors.Is(err, errWorkerPin) || report.started {
			t.Fatalf("err=%v report=%+v", err, report)
		}
	})

	t.Run("symlinked worker refused", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "link-worker")
		if err := os.Symlink(pinned, link); err != nil {
			t.Fatal(err)
		}
		l, err := newWorkerLauncher(config(link, digest, time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if report, err := l.run(context.Background(), "1", func(Frame, func(Frame) error) error { return nil }); !errors.Is(err, errWorkerPin) || report.started {
			t.Fatalf("err=%v report=%+v", err, report)
		}
	})

	// Real negative control: a correctly pinned binary that does NOT seal itself
	// (no seccomp, no limits) must be killed and reaped before any source is
	// written. /bin/cat would otherwise echo the source back as stdout frames.
	t.Run("unsealed binary killed before source", func(t *testing.T) {
		l, err := newWorkerLauncher(config("/bin/cat", fileSHA256(t, "/bin/cat"), 3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		accepted := 0
		start := time.Now()
		report, err := l.run(context.Background(), "SOURCE-MUST-NOT-BE-SENT", func(Frame, func(Frame) error) error {
			accepted++
			return nil
		})
		if !errors.Is(err, errWorkerAttestation) || !report.started || report.attested || report.sourceWritten || !report.waited || accepted != 0 {
			t.Fatalf("err=%v report=%+v accepted=%d", err, report, accepted)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("attestation refusal relied on wall deadline: %v", time.Since(start))
		}
		t.Logf("unsealed binary refused: %v after %v, waited=%v", err, time.Since(start), report.waited)
	})

	t.Run("wall deadline kills blocked worker", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, 500*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		session, _ := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
		defer session.Retire()
		start := time.Now()
		report, err := l.run(context.Background(), twoCalls, func(frame Frame, _ func(Frame) error) error {
			_, e := session.Accept(frame)
			return e // never reply: worker blocks on IPC
		})
		elapsed := time.Since(start)
		if !errors.Is(err, errWorkerWallTimeout) || !report.waited || elapsed > 2*time.Second {
			t.Fatalf("err=%v report=%+v elapsed=%v", err, report, elapsed)
		}
		t.Logf("blocked worker killed at wall deadline and waited after %v", elapsed)
	})

	t.Run("caller cancellation kills and waits", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, 3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancelCause(context.Background())
		reason := errors.New("caller retired execution")
		session, _ := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
		defer session.Retire()
		report, err := l.run(ctx, twoCalls, func(frame Frame, _ func(Frame) error) error {
			if _, e := session.Accept(frame); e != nil {
				return e
			}
			cancel(reason)
			return nil
		})
		if !errors.Is(err, reason) || !report.waited {
			t.Fatalf("err=%v report=%+v", err, report)
		}
	})

	t.Run("cpu loop bounded by attested limit", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, 5*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		report, err := l.run(context.Background(), `for(;;){}`, func(Frame, func(Frame) error) error { return nil })
		elapsed := time.Since(start)
		if err == nil || errors.Is(err, errWorkerWallTimeout) || !report.waited || elapsed > 3*time.Second {
			t.Fatalf("err=%v report=%+v elapsed=%v", err, report, elapsed)
		}
		t.Logf("cpu loop terminated by kernel RLIMIT_CPU: %v after %v", err, elapsed)
	})

	t.Run("memory pressure fails inside bounds", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, 5*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		session, _ := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
		defer session.Retire()
		var failure bool
		start := time.Now()
		report, err := l.run(context.Background(), `const a=[];for(;;){a.push(new ArrayBuffer(1<<20));}`, func(frame Frame, _ func(Frame) error) error {
			message, e := session.Accept(frame)
			if e == nil && message.Failure != nil {
				failure = true
			}
			return e
		})
		elapsed := time.Since(start)
		if errors.Is(err, errWorkerWallTimeout) || !report.waited || !report.attested || elapsed > 3*time.Second {
			t.Fatalf("err=%v report=%+v elapsed=%v", err, report, elapsed)
		}
		if err == nil && !failure {
			t.Fatal("allocation flood reported success")
		}
		t.Logf("allocation flood bounded: err=%v failure_frame=%v after %v (attested AS hard=%d)", err, failure, elapsed, report.attestation.asHard)
	})

	// Regression for sourceWritten: an attested worker whose stdin has no reader
	// makes the source write fail (EPIPE); the report must say not written.
	t.Run("failed source write reported not written", func(t *testing.T) {
		closer := os.Getenv("BRAIN_NATIVE_STDIN_CLOSER_FIXTURE")
		if closer == "" {
			t.Fatal("stdin-closer fixture not supplied by the Linux wrapper")
		}
		pinnedCloser := filepath.Join(t.TempDir(), "stdin-closer")
		closerData, err := os.ReadFile(closer)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pinnedCloser, closerData, 0o555); err != nil {
			t.Fatal(err)
		}
		l, err := newWorkerLauncher(config(pinnedCloser, fileSHA256(t, pinnedCloser), 3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		report, err := l.run(context.Background(), "1", func(Frame, func(Frame) error) error { return errors.New("no frames expected") })
		if err == nil || errors.Is(err, errWorkerAttestation) || errors.Is(err, errWorkerWallTimeout) {
			t.Fatalf("want source write failure, got err=%v", err)
		}
		if !report.attested || report.sourceWritten || !report.waited {
			t.Fatalf("failed write must report sourceWritten=false: %+v", report)
		}
		t.Logf("source write failed after attestation: err=%v sourceWritten=%v waited=%v in %v", err, report.sourceWritten, report.waited, time.Since(start))
	})

	t.Run("oversized source refused", func(t *testing.T) {
		l, err := newWorkerLauncher(config(pinned, digest, time.Second))
		if err != nil {
			t.Fatal(err)
		}
		report, err := l.run(context.Background(), strings.Repeat(" ", launcherMaxSourceBytes+1)+"1", func(Frame, func(Frame) error) error { return nil })
		if !errors.Is(err, errLauncherConfig) || report.started {
			t.Fatalf("err=%v report=%+v", err, report)
		}
	})
}

// Host wrapper: cross-builds the Go test parent for Linux and runs the native
// launcher suite inside the opt-in isolated compiler container as uid 65534.
func TestQuickJSLauncherLinux(t *testing.T) {
	goarch := os.Getenv("BRAIN_SCRIPT_LINUX_GOARCH") // e.g. amd64 for an x86_64 VM
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		binary := filepath.Join(t.TempDir(), "launcher.test")
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
		defer cancel()
		build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
		if out, e := build.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("cross-build native parent: %w %s", e, out)
		}
		data, e := os.ReadFile(binary)
		if e != nil {
			return nil, e
		}
		copyCmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", "cat > /tmp/launcher.test && chmod 755 /tmp/launcher.test")
		copyCmd.Stdin = bytes.NewReader(data)
		if out, e := copyCmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("copy native parent: %w %s", e, out)
		}
		// Independent relocated rebuild with the same pinned recipe: its digest,
		// not a hash of the artifact under test, becomes the launch pin.
		rebuild := exec.CommandContext(ctx, "docker", "--host", host, "exec", name, "/bin/sh", "-c", `set -eu
mkdir /tmp/pin-rebuild
cp /tmp/probe.c /tmp/worker.c /tmp/seal.h /tmp/pin-rebuild/
cp -R /tmp/quickjs-2026-06-04 /tmp/pin-rebuild/
/bin/sh /tmp/build-probe.sh /tmp/pin-rebuild worker.c /tmp/pin-rebuild/worker >/dev/null
sha256sum /tmp/pin-rebuild/worker | cut -d' ' -f1`)
		pinOut, e := rebuild.CombinedOutput()
		pin := strings.TrimSpace(string(pinOut))
		if e != nil || !lowerHexSHA256(pin) {
			return nil, fmt.Errorf("independent pin rebuild: %v %s", e, pinOut)
		}
		// The committed release record is the operator pin: this session's
		// rebuild must reproduce it exactly, not merely itself.
		platform := "linux/" + goarch
		recorded := loadScriptWorkerRelease(t).Outputs[platform]
		if ver, _ := exec.CommandContext(ctx, "docker", "--host", host, "exec", name, "/bin/sh", "-c", "/usr/bin/cc --version | head -1").CombinedOutput(); len(ver) > 0 {
			t.Logf("compiler: %s", strings.TrimSpace(string(ver)))
		}
		if recorded != pin {
			return nil, fmt.Errorf("release.json outputs[%q]=%q but reproducible build produced %s", platform, recorded, pin)
		}
		closerSource, e := os.ReadFile("testdata/stdin_closer.c")
		if e != nil {
			return nil, e
		}
		closer := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", "cat > /tmp/stdin_closer.c && cc -O2 -D_GNU_SOURCE -I/tmp /tmp/stdin_closer.c -o /tmp/stdin-closer")
		closer.Stdin = bytes.NewReader(closerSource)
		if out, e := closer.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("build stdin-closer fixture: %w %s", e, out)
		}
		run := exec.CommandContext(ctx, "docker", "--host", host, "exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe", "BRAIN_NATIVE_STDIN_CLOSER_FIXTURE=/tmp/stdin-closer", "BRAIN_NATIVE_WORKER_PIN="+pin, "/tmp/launcher.test", "-test.run=^TestNativeLauncher(Pool)?$", "-test.v", "-test.timeout=60s")
		out, e := run.CombinedOutput()
		if e != nil {
			return nil, fmt.Errorf("native launcher: %w %s", e, out)
		}
		t.Logf("real Linux launcher fixture (non-race cross-build):\n%s", out)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
