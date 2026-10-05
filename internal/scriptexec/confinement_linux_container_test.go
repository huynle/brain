package scriptexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Deliberately an opt-in local experiment, never a fallback execution backend.
// No image is pulled, no host filesystem is mounted, and only the container
// created by this test is removed. A Unix socket is required to avoid remote
// infrastructure changes from an ambient Docker context.
func linuxPrototypeRun(t *testing.T, script string) ([]byte, string, func(...string) ([]byte, error), error) {
	t.Helper()
	host := os.Getenv("BRAIN_SCRIPT_LINUX_PROTOTYPE_HOST")
	image := os.Getenv("BRAIN_SCRIPT_LINUX_PROTOTYPE_IMAGE")
	if host == "" || image == "" {
		t.Skip("opt-in local Linux container experiment, not a release gate")
	}
	if !strings.HasPrefix(host, "unix:///") || !strings.HasPrefix(image, "sha256:") {
		t.Fatal("prototype requires a local Unix Docker socket and immutable image digest")
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", host}, args...)...)
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	// Inspect only: never implicitly pull an absent image.
	if out, err := run("image", "inspect", image, "--format", "{{.Id}}"); err != nil {
		t.Fatalf("local image unavailable: %v %s", err, out)
	}
	name := fmt.Sprintf("brain-script-probe-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if out, err := run("rm", "-f", name); err != nil {
			t.Errorf("owned prototype container cleanup failed: %v %s", err, out)
		}
	})
	out, err := run("run", "--name", name, "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=128m", "--cpus=0.5", "--user=65534:65534", "--platform=linux/amd64", "--entrypoint=/usr/bin/env", image, "-i", "/usr/local/bin/node", "--max-old-space-size=16", "-e", script)
	return out, name, run, err
}

func TestLinuxContainerPrototypeReportsProcessGap(t *testing.T) {
	out, _, _, err := linuxPrototypeRun(t, `const fs=require('fs'),cp=require('child_process');
(async()=>{const r={asyncResult:await Promise.resolve(42),environment:Object.keys(process.env).length};
try{fs.writeFileSync('/probe','x');r.write='ALLOWED'}catch(e){r.write=e.code}
r.spawn=cp.spawnSync('/bin/echo',['child-was-allowed']).stdout.toString().trim();
for(const key of ['memory.max','cpu.max','pids.max'])r[key]=fs.readFileSync('/sys/fs/cgroup/'+key,'utf8').trim();
console.log(JSON.stringify(r));})()`)
	if err != nil {
		t.Fatalf("container probe failed: %v %s", err, out)
	}
	var r map[string]any
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode: %v %s", err, out)
	}
	if r["asyncResult"] != float64(42) || r["environment"] != float64(0) || r["write"] != "EROFS" || r["memory.max"] != "134217728" || r["cpu.max"] != "50000 100000" || r["pids.max"] != "32" {
		t.Fatalf("limits/environment mismatch: %s", out)
	}
	if r["spawn"] != "child-was-allowed" {
		t.Fatalf("process-gap negative control changed; investigate instead of declaring confinement: %s", out)
	}
	t.Logf("observed cgroup limits/read-only root; arbitrary process execution STILL ALLOWED, profile NOT eligible: %s", out)
}

func TestLinuxContainerPrototypeMemoryPressure(t *testing.T) {
	out, name, run, err := linuxPrototypeRun(t, `const held=[];for(;;)held.push(Buffer.alloc(4*1024*1024,1));`)
	if err == nil {
		t.Fatalf("memory pressure unexpectedly completed: %s", out)
	}
	state, inspectErr := run("inspect", name, "--format", "{{.State.OOMKilled}} {{.State.ExitCode}} {{.State.Status}}")
	if inspectErr != nil || strings.TrimSpace(string(state)) != "true 137 exited" {
		t.Fatalf("not a proven cgroup OOM termination: run=%v output=%s inspect=%v state=%s", err, out, inspectErr, state)
	}
	t.Log("128MiB cgroup terminated external-buffer pressure: OOMKilled=true exit=137 exited; owned container cleanup follows")
}
