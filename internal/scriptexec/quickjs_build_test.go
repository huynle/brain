package scriptexec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestQuickJSExperimentalBuildHardening(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", name, "/bin/sh", "-c", "readelf -h -l -d -W /tmp/probe; readelf -s -W /tmp/probe; /usr/bin/cc --version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("inspect ELF: %w %s", err, out)
		}
		for _, required := range []string{"DYN (Position-Independent Executable file)", "GNU_RELRO", "BIND_NOW", "__stack_chk_fail", "__memcpy_chk"} {
			if !strings.Contains(string(out), required) {
				t.Errorf("experimental worker missing actual ELF hardening: %s", required)
			}
		}
		stackSeen := false
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "GNU_STACK") {
				stackSeen = true
				fields := strings.Fields(line)
				if len(fields) != 8 || fields[6] != "RW" {
					t.Errorf("stack must be writable/non-executable: %s", line)
				}
			}
		}
		if !stackSeen {
			t.Error("missing explicit GNU_STACK policy")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQuickJSExperimentalBuildReproducible(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		// Different source and output paths, not just rerunning the same command.
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", name, "/bin/sh", "-c", `set -eu
mkdir /tmp/relocated
cp /tmp/probe.c /tmp/worker.c /tmp/relocated/
cp -R /tmp/quickjs-2026-06-04 /tmp/relocated/
/bin/sh /tmp/build-probe.sh /tmp/relocated worker.c /tmp/relocated/worker
cmp /tmp/probe /tmp/relocated/worker
sha256sum /tmp/probe /tmp/relocated/worker
/usr/bin/cc --version | head -1
`)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("relocated build is not reproducible: %w %s", err, out)
		}
		t.Logf("experimental artifact reproducibility (not runtime approval): image=%s\n%s", os.Getenv("BRAIN_QUICKJS_PROBE_IMAGE"), out)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
