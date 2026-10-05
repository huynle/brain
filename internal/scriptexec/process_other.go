//go:build !linux

package scriptexec

import "os/exec"

// No parent-death guarantee on this platform. This package remains inactive;
// supported-platform confinement and lifecycle approval are separate launch gates.
func prepareWorkerParentDeath(_ *exec.Cmd) func() { return func() {} }
