package scriptexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// systemdReapScript runs on a Linux host whose PID 1 is systemd (local test VM
// only). It starts the server stand-in as a TRANSIENT unit with
// KillMode=control-group, SIGKILLs only the unit's main PID, and reports what
// happened to the sealed worker. In this topology the unit's cgroup kill and
// the launcher's PDEATHSIG both end the worker; PDEATHSIG alone is isolated by
// TestQuickJSInitReaping. Nothing is installed or left running.
const systemdReapScript = `set -eu
DIR="$1"; UNIT=brain-g-reap-test-$$; MARK=/tmp/$UNIT.marker; U=$(id -u)
# Always tear the transient unit down, even if a step fails: SIGKILL every
# process in its cgroup (server, worker, holder), then stop and reset it.
cleanup() {
  sudo systemctl kill --kill-whom=all -s SIGKILL "$UNIT" 2>/dev/null || true
  sudo systemctl stop "$UNIT" 2>/dev/null || true
  sudo systemctl reset-failed "$UNIT" 2>/dev/null || true
  rm -f "$MARK"
}
trap cleanup EXIT
sudo systemd-run --quiet --unit "$UNIT" -p KillMode=control-group --uid="$U" \
  --setenv=BRAIN_NATIVE_WORKER_FIXTURE="$DIR/worker" --setenv=BRAIN_NATIVE_HOLD_MARKER="$MARK" \
  "$DIR/launcher.test" '-test.run=^TestNativeLauncherServerHold$' -test.timeout=10m
i=0; until [ -s "$MARK" ]; do i=$((i+1)); [ $i -gt 300 ] && { echo "no marker"; exit 1; }; sleep 0.2; done
SERVER=$(sed -n 's/^server=\([0-9]*\) .*/\1/p' "$MARK"); WORKER=$(sed -n 's/.* worker=\([0-9]*\) .*/\1/p' "$MARK"); HOLDER=$(sed -n 's/.* holder=\([0-9]*\)$/\1/p' "$MARK")
echo "pid1=$(cat /proc/1/comm) mainpid=$(systemctl show -p MainPID --value "$UNIT") server=$SERVER worker=$WORKER worker_ppid=$(awk '{print $4}' /proc/$WORKER/stat)"
echo "worker_status $(grep -E '^(NoNewPrivs|Seccomp|Seccomp_filters):' /proc/$WORKER/status | tr -s '\t\n' '  ')"
CG=$(systemctl show -p ControlGroup --value "$UNIT")
sudo kill -9 "$SERVER"; sleep 2
# Topology verdict BEFORE any cleanup: a live (non-zombie) process here means
# the unit/launcher left it running after the server died.
alive() { [ -e /proc/$1 ] && [ "$(awk '{print $3}' /proc/$1/stat 2>/dev/null)" != Z ] && echo 1 || echo 0; }
CGPROCS=0; [ -n "$CG" ] && [ -f "/sys/fs/cgroup$CG/cgroup.procs" ] && CGPROCS=$(wc -l < "/sys/fs/cgroup$CG/cgroup.procs")
if [ -e /proc/$WORKER ]; then echo "WORKER_STATE=$(awk '{print $3}' /proc/$WORKER/stat)"; else echo "WORKER_STATE=gone"; fi
echo "post_kill alive_server=$(alive $SERVER) alive_worker=$(alive $WORKER) alive_holder=$(alive $HOLDER) cgroup_procs=$CGPROCS"
echo "unit_state=$(systemctl show -p ActiveState --value "$UNIT") result=$(systemctl show -p Result --value "$UNIT")"
# Cleanup is separate evidence: it must leave nothing behind either way.
cleanup; sleep 1
LEFT=0; for p in $SERVER $WORKER $HOLDER; do if [ -e /proc/$p ]; then LEFT=$((LEFT+1)); fi; done
echo "leftover_units=$(systemctl list-units --all "$UNIT*" --no-legend | wc -l) leftover_procs=$LEFT"
`

// TestLinuxSystemdReaping is opt-in: BRAIN_SCRIPT_SYSTEMD_SHELL is a command
// prefix that runs a shell on a systemd-PID1 Linux test host with passwordless
// sudo (e.g. "colima ssh --"); BRAIN_SCRIPT_SYSTEMD_DIR is a directory visible at
// the same path on both sides; BRAIN_SCRIPT_WORKER_ARTIFACT is a worker whose
// digest is recorded in release.json for that host's architecture.
func TestLinuxSystemdReaping(t *testing.T) {
	prefix, dir, artifact := os.Getenv("BRAIN_SCRIPT_SYSTEMD_SHELL"), os.Getenv("BRAIN_SCRIPT_SYSTEMD_DIR"), os.Getenv("BRAIN_SCRIPT_WORKER_ARTIFACT")
	if prefix == "" || dir == "" || artifact == "" {
		t.Skip("opt-in systemd PID1 reaping test")
	}
	goarch := os.Getenv("BRAIN_SCRIPT_LINUX_GOARCH")
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if digest := hex.EncodeToString(sum[:]); loadScriptWorkerRelease(t).Outputs["linux/"+goarch] != digest {
		t.Fatalf("artifact %s is not the recorded linux/%s release output", digest, goarch)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "test", "-c", "-o", filepath.Join(dir, "launcher.test"), ".")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cross-build: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "reap.sh")
	if err := os.WriteFile(script, []byte(systemdReapScript), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append(strings.Fields(prefix), "sh", script, dir)
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	text := string(out)
	t.Logf("systemd reaping output:\n%s", text)
	if err != nil {
		t.Fatalf("systemd scenario: %v", err)
	}
	for _, want := range []string{"pid1=systemd", "NoNewPrivs: 1", "Seccomp: 2", "WORKER_STATE=gone", "post_kill alive_server=0 alive_worker=0 alive_holder=0 cgroup_procs=0", "result=signal", "leftover_units=0 leftover_procs=0"} {
		if !strings.Contains(text, want) {
			t.Errorf("systemd evidence missing %q", want)
		}
	}
}
