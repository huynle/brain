//go:build !linux

package scriptexec

import (
	"context"
	"os"
)

// Native macOS and every other non-Linux OS are unsupported (LINUX-FIRST-
// 20261006): no enforceable memory/confinement boundary has been proven here.
// There is no degraded fallback; newWorkerLauncher refuses before reaching these.
func openPinnedWorker(string, string) (*os.File, error) { return nil, errLauncherUnsupported }

func attestWorker(context.Context, int, *os.File) (workerAttestation, error) {
	return workerAttestation{}, errLauncherUnsupported
}
