//go:build darwin

package scriptexec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// PASS means the suspected memory-isolation gap was reproduced, NOT safety.
func TestDarwinAddressSpaceDoesNotBoundResidentMemory(t *testing.T) {
	if os.Getenv("BRAIN_SCRIPT_DARWIN_MEMORY_PROBE") != "1" {
		t.Skip("opt-in bounded native macOS negative experiment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "resident-probe")
	if out, e := exec.CommandContext(ctx, "cc", "-O1", "-Wall", "-Wextra", "testdata/darwin_resident_probe.c", "-o", binary).CombinedOutput(); e != nil {
		t.Fatalf("compile: %v %s", e, out)
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = []string{}
	cmd.WaitDelay = time.Second
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("bounded native probe: %v %s", e, out)
	}
	var result struct{ Requested, Delta uint64 }
	var raw map[string]uint64
	if e = json.Unmarshal(out, &raw); e != nil {
		t.Fatalf("decode: %v %s", e, out)
	}
	result.Requested = raw["requested"]
	result.Delta = raw["rss_delta"]
	if result.Requested != 128*1024*1024 || result.Delta <= 64*1024*1024 || raw["vm_after"]-raw["vm_before"] > 64*1024*1024 {
		t.Fatalf("allocator gap not reproduced; investigate rather than certify: %s", out)
	}
	t.Logf("UNSAFE negative control, baseline+64MiB RLIMIT_AS allowed >64MiB new resident memory: %s", out)
}
