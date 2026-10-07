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

// This tests absence of an unprivileged mechanism, not a usable memory limit.
// It never asks for privilege/entitlement and changes only the probe's own PID.
func TestDarwinMemlimitRequiresPrivilege(t *testing.T) {
	if os.Getenv("BRAIN_SCRIPT_DARWIN_MEMORY_PROBE") != "1" {
		t.Skip("opt-in native macOS capability investigation")
	}
	if os.Geteuid() == 0 {
		t.Skip("never run the privilege-refusal probe as root")
	}
	const source = `
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <errno.h>
#include <unistd.h>
int main(void) {
    typedef int (*control_fn)(uint32_t,int32_t,uint32_t,void*,size_t);
    control_fn control=(control_fn)dlsym(RTLD_DEFAULT,"memorystatus_control");
    if(!control)return 77;
    // XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea kern_memorystatus.h:
    // command7, active/inactive MB + attrs. Zero attrs; no allocator stress.
    struct {int32_t active;uint32_t active_attr;int32_t inactive;uint32_t inactive_attr;} limits={64,0,64,0};
    errno=0;
    int rc=control(7,getpid(),0,&limits,sizeof(limits));
    int error=errno;
    printf("{\"return\":%d,\"errno\":%d}\n",rc,error);
    return 0;
}
`
	dir := t.TempDir()
	file := filepath.Join(dir, "memlimit.c")
	binary := filepath.Join(dir, "memlimit")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", file, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = []string{}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("memlimit probe: %v %s", err, out)
	}
	var result struct {
		Return int `json:"return"`
		Errno  int `json:"errno"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if result.Return != -1 || result.Errno != 1 {
		t.Fatalf("unprivileged memlimit assumption changed; investigate, do not enable: %s", out)
	}
	t.Logf("UNAVAILABLE without privilege: memorystatus self-limit refused EPERM: %s", out)
}
