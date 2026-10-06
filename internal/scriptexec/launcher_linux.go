package scriptexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"syscall"
	"time"
)

// openPinnedWorker opens the configured worker without following a final
// symlink, checks ownership/mode and hashes THIS descriptor. The launcher execs
// /proc/self/fd/<n>, so a later path swap cannot substitute another binary.
// Production installs the worker read-only and root-owned; a writer that can
// modify the pinned inode in place is outside this boundary.
func openPinnedWorker(path, digest string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkerPin
	}
	fail := func() (*os.File, error) { _ = f.Close(); return nil, errWorkerPin }
	info, err := f.Stat()
	if err != nil {
		return fail()
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fail()
	}
	facts := workerFileFacts{
		regular: info.Mode().IsRegular(),
		mode:    uint32(st.Mode) & 0o7777,
		uid:     st.Uid,
		setuid:  info.Mode()&os.ModeSetuid != 0,
		setgid:  info.Mode()&os.ModeSetgid != 0,
	}
	if checkWorkerFileFacts(facts, uint32(os.Geteuid())) != nil {
		return fail()
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, info.Size())); err != nil {
		return fail()
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fail()
	}
	return f, nil
}

func procStatus(pid string) (string, error) {
	data, err := os.ReadFile("/proc/" + pid + "/status")
	return string(data), err
}

// attestWorker waits (bounded) until the child has installed its own seccomp
// filter, which the worker does LAST after descriptor closure and limits, and
// then verifies the final state. Before this returns nil no source is written.
func attestWorker(ctx context.Context, pid int, worker *os.File) (workerAttestation, error) {
	self, err := procStatus("self")
	if err != nil {
		return workerAttestation{}, errWorkerAttestation
	}
	inherited, ok := statusSeccompFilters(self)
	if !ok {
		return workerAttestation{}, errWorkerAttestation // kernel lacks Seccomp_filters
	}
	id := strconv.Itoa(pid)
	deadline := time.NewTimer(launcherAttestTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		status, err := procStatus(id)
		if err != nil {
			return workerAttestation{}, errWorkerAttestation
		}
		if n, ok := statusSeccompFilters(status); ok && n > inherited {
			break
		}
		select {
		case <-ctx.Done():
			return workerAttestation{}, context.Cause(ctx)
		case <-deadline.C:
			return workerAttestation{}, errWorkerAttestation
		case <-tick.C:
		}
	}
	// The seal is irreversible (setrlimit/prctl/seccomp are denied), so these
	// reads observe the final launch state.
	status, err := procStatus(id)
	if err != nil {
		return workerAttestation{}, errWorkerAttestation
	}
	limits, err := os.ReadFile("/proc/" + id + "/limits")
	if err != nil {
		return workerAttestation{}, errWorkerAttestation
	}
	a, err := parseWorkerAttestation(status, string(limits))
	if err != nil {
		return a, err
	}
	if err := a.check(inherited); err != nil {
		return a, err
	}
	dir, err := os.Open("/proc/" + id + "/fd")
	if err != nil {
		return a, errWorkerAttestation
	}
	names, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil || checkWorkerDescriptors(names) != nil {
		return a, errWorkerAttestation
	}
	exe, err := os.Stat("/proc/" + id + "/exe")
	pinned, perr := worker.Stat()
	if err != nil || perr != nil || !os.SameFile(exe, pinned) {
		return a, errWorkerAttestation
	}
	return a, nil
}
