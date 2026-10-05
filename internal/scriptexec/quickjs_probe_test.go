package scriptexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Experimental only: requires an explicitly supplied official source archive
// and an already installed local image. No downloads, host mounts or services.
func quickJSProbe(t *testing.T, injection string) ([]byte, error) {
	return quickJSProgram(t, injection, false, nil)
}

func quickJSProgram(t *testing.T, injection string, worker bool, exercise func(string, string) ([]byte, error)) ([]byte, error) {
	t.Helper()
	archive := os.Getenv("BRAIN_QUICKJS_PROBE_ARCHIVE")
	host := os.Getenv("BRAIN_SCRIPT_LINUX_PROTOTYPE_HOST")
	image := os.Getenv("BRAIN_QUICKJS_PROBE_IMAGE")
	if archive == "" || host == "" || image == "" {
		t.Skip("opt-in embedded-runtime confinement investigation")
	}
	if !strings.HasPrefix(host, "unix:///") || !strings.HasPrefix(image, "sha256:") {
		t.Fatal("local Unix Docker socket and immutable image required")
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "b376e839b322978313d929fd20663b11ba58b75df5a46c126dd19ea2fa70ad2a" {
		t.Fatal("QuickJS2026-06-04 archive digest mismatch")
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", host}, args...)...)
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	if out, err := run("image", "inspect", image); err != nil {
		t.Fatalf("installed image: %v %s", err, out)
	}
	name := fmt.Sprintf("brain-quickjs-probe-%d", time.Now().UnixNano())
	if out, err := run("create", "--name", name, "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=512m", "--cpus=1", "--tmpfs=/tmp:rw,exec,nosuid,size=128m,mode=1777", "--entrypoint=/bin/sleep", image, "600"); err != nil {
		t.Fatalf("create: %v %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := run("rm", "-f", name); err != nil {
			t.Errorf("owned container cleanup: %v %s", err, out)
		}
	})
	if out, err := run("start", name); err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	copyInput := func(path string, content []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", `cat > "$1"`, "copy", path)
		cmd.Stdin = bytes.NewReader(content)
		cmd.WaitDelay = time.Second
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tmpfs copy: %v %s", err, out)
		}
	}
	copyInput("/tmp/source.tar.xz", data)
	probe, err := os.ReadFile("testdata/quickjs_probe.c")
	if err != nil {
		t.Fatal(err)
	}
	if injection != "" {
		anchor := []byte("    errno=0; int opened=")
		if bytes.Count(probe, anchor) != 1 {
			t.Fatal("native injection anchor changed")
		}
		probe = bytes.Replace(probe, anchor, append([]byte(injection), anchor...), 1)
	}
	copyInput("/tmp/probe.c", probe)
	entry := "../probe.c"
	if worker {
		content, err := os.ReadFile("testdata/quickjs_worker.c")
		if err != nil {
			t.Fatal(err)
		}
		copyInput("/tmp/worker.c", content)
		entry = "../worker.c"
	}
	build := `cd /tmp && tar --no-same-owner -xf source.tar.xz && cd quickjs-2026-06-04 && cc -O1 -D_GNU_SOURCE -DCONFIG_VERSION='"2026-06-04"' -I. ` + entry + ` quickjs.c dtoa.c libregexp.c libunicode.c cutils.c -lm -o /tmp/probe`
	if out, err := run("exec", name, "/bin/sh", "-c", build); err != nil {
		t.Fatalf("build probe: %v %s", err, out)
	}
	if exercise != nil {
		return exercise(host, name)
	}
	return run("exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe")
}

func TestQuickJSNativeAddressSpaceProbe(t *testing.T) {
	out, err := quickJSProbe(t, `
    errno=0; void *small=mmap(NULL,4096,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS,-1,0);
    int small_ok=(small!=MAP_FAILED); if(small_ok)munmap(small,4096);
    errno=0; void *large=mmap(NULL,128*1024*1024,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS,-1,0);
    int large_errno=errno; if(large!=MAP_FAILED)munmap(large,128*1024*1024);
    printf("{\"small_ok\":%d,\"large_errno\":%d}\n",small_ok,large_errno); _exit(0);
`)
	if err != nil {
		t.Fatalf("memory probe failed: %v %s", err, out)
	}
	var got map[string]int
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode: %v %s", err, out)
	}
	if got["small_ok"] != 1 || got["large_errno"] != 12 {
		t.Fatalf("native address-space bound absent or memory entirely denied: %s (want small_ok=1 large_errno=ENOMEM12)", out)
	}
	t.Logf("actual native allocation bound: %s", out)
}

func TestQuickJSNativeConfinementProbe(t *testing.T) {
	out, err := quickJSProbe(t, "")
	if err != nil {
		t.Fatalf("probe process: %v %s", err, out)
	}
	var got map[string]int
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("probe JSON: %v %s", err, out)
	}
	if got["async"] != 42 {
		t.Fatalf("async evaluation failed: %s", out)
	}
	for _, key := range []string{"open", "write", "socket", "fork", "read_fd", "pread_fd", "readv_fd", "dup_fd", "mmap_fd", "mmap_exec"} {
		if got[key] != 1 {
			t.Errorf("native %s escaped deny-default boundary: errno=%d (want EPERM=1); %s", key, got[key], out)
		}
	}
	t.Logf("embedded async + native syscall observations: %s", out)
}
