package scriptexec

import (
	"os/exec"
	"runtime"
	"syscall"
)

// Linux associates PDEATHSIG with the creating THREAD. Keep that thread alive
// through Wait, not merely through Start. Go installs the signal before exec and
// checks the pre-fork parent PID after prctl, closing the parent-death setup race.
// The trusted launch owner must still prohibit set-ID exec and require the native
// child's syscall policy to deny changing this signal. An external init/subreaper
// must reap if this process dies; this primitive cannot reap after its own death.
func prepareWorkerParentDeath(cmd *exec.Cmd) func() {
	runtime.LockOSThread()
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	attr.Pdeathsig = syscall.SIGKILL
	cmd.SysProcAttr = &attr
	return runtime.UnlockOSThread
}
