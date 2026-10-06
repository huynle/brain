package scriptexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Linux-first (LINUX-FIRST-20261006) production launch policy for the sealed
// QuickJS worker. DISABLED by default: the zero launcherConfig refuses to
// construct, and every non-Linux OS (including native macOS) reports
// unsupported. No route, config key, capability or caller is wired; activation
// remains gated on C-F integration and independent acceptance. This is not
// hosted D06 VM isolation.
//
// Per launch it (1) re-verifies the pinned worker binary from an O_NOFOLLOW
// descriptor and executes THAT inode via /proc/self/fd, (2) starts it with an
// empty environment, cwd "/", only stdin/stdout/stderr and parent-death SIGKILL,
// (3) attests from /proc, before any source is written, that the child installed
// its own seccomp filter on top of any inherited one, has NoNewPrivs, kernel-
// enforced CPU/address-space ceilings, exactly descriptors 0-2 and the pinned
// executable, and (4) kills and Waits on attestation failure, wall deadline,
// caller cancellation, protocol violation or diagnostic flood.

var (
	errLauncherDisabled    = errors.New("script worker launcher disabled")
	errLauncherUnsupported = errors.New("script worker launcher unsupported on this platform")
	errLauncherConfig      = errors.New("script worker launcher request or configuration refused")
	errWorkerPin           = errors.New("script worker binary pin refused")
	errWorkerAttestation   = errors.New("script worker confinement attestation failed")
	errWorkerWallTimeout   = errors.New("script worker wall deadline exceeded")
)

const (
	launcherMaxWall             = 30 * time.Second
	launcherAddressSpaceCeiling = 64 << 20
	launcherCPUCeilingSeconds   = 1
	launcherMaxSourceBytes      = 32 << 10 // equals the worker's SOURCE_LIMIT
	launcherWireBudget          = 1 << 20  // total stdout wire bytes per execution
	launcherAttestTimeout       = time.Second
)

type launcherConfig struct {
	Enabled      bool
	WorkerPath   string
	WorkerSHA256 string
	WallTimeout  time.Duration
}

func (c launcherConfig) validate() error {
	if c.WorkerPath == "" || !filepath.IsAbs(c.WorkerPath) || filepath.Clean(c.WorkerPath) != c.WorkerPath {
		return errLauncherConfig
	}
	if !lowerHexSHA256(c.WorkerSHA256) || c.WallTimeout <= 0 || c.WallTimeout > launcherMaxWall {
		return errLauncherConfig
	}
	return nil
}

// launcherAvailability is a content-free reason for discovery/operators. Even
// "configured" is not execution availability: each launch must still attest.
func launcherAvailability(c launcherConfig) string {
	switch {
	case !c.Enabled:
		return "disabled"
	case c.validate() != nil:
		return "misconfigured"
	case runtime.GOOS != "linux":
		return "unsupported_platform"
	}
	return "configured"
}

type workerLauncher struct{ cfg launcherConfig }

func newWorkerLauncher(c launcherConfig) (*workerLauncher, error) {
	if !c.Enabled {
		return nil, errLauncherDisabled
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" {
		return nil, errLauncherUnsupported
	}
	return &workerLauncher{cfg: c}, nil
}

type launchReport struct {
	started, attested, sourceWritten, waited bool
	attestation                              workerAttestation
}

// run executes one fresh worker for one source. accept receives every stdout
// frame (it must validate via ProtocolSession and quarantine output; it is not
// authorization) and may answer calls through reply. The returned error is the
// cancellation cause when the worker was killed.
//
// A nil error means only that the worker exited 0 with intact framing. It does
// NOT mean a terminal result was produced: callers must separately require the
// ProtocolSession terminal outcome (WorkerMessage.Done or Failure) and treat
// its absence as a failed execution.
func (l *workerLauncher) run(ctx context.Context, source string, accept func(frame Frame, reply func(Frame) error) error) (launchReport, error) {
	var report launchReport
	if source == "" || len(source) > launcherMaxSourceBytes || !utf8.ValidString(source) {
		return report, errLauncherConfig
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return report, errLauncherConfig
	}
	worker, err := openPinnedWorker(l.cfg.WorkerPath, l.cfg.WorkerSHA256)
	if err != nil {
		return report, err
	}
	defer worker.Close()
	ctx, stop := context.WithTimeoutCause(ctx, l.cfg.WallTimeout, errWorkerWallTimeout)
	defer stop()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cmd := exec.Command(fmt.Sprintf("/proc/self/fd/%d", worker.Fd()))
	cmd.Args = []string{"brain-script-worker"}
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return report, err
	}
	var writeMu sync.Mutex
	reply := func(f Frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return WriteFrame(stdin, f)
	}
	var written, sent atomic.Bool
	sink := &frameSink{remaining: launcherWireBudget, cancel: cancel, accept: func(f Frame) error {
		if !written.Load() {
			return ErrProtocol // nothing may precede the source frame
		}
		return accept(f, reply)
	}}
	cmd.Stdout = sink
	err = runWorkerProcessWithStart(ctx, cmd, func(pid int) error {
		report.started = true
		attestation, err := attestWorker(ctx, pid, worker)
		report.attestation = attestation
		if err != nil {
			return err
		}
		report.attested = true
		written.Store(true) // admit worker frames once the source write begins
		if err := reply(Frame{Version: 1, Kind: "call", Sequence: 1, Payload: encoded}); err != nil {
			return err
		}
		sent.Store(true)
		return nil
	})
	report.waited = cmd.ProcessState != nil
	report.sourceWritten = sent.Load()
	if err == nil {
		err = sink.finish()
	}
	return report, err
}

type workerAttestation struct {
	noNewPrivs                       bool
	seccompMode, seccompFilters      int
	cpuSoft, cpuHard, asSoft, asHard uint64
}

const limitUnlimited = ^uint64(0)

// parseWorkerAttestation reads /proc/<pid>/status and /proc/<pid>/limits text.
// Missing fields fail closed (Seccomp_filters requires Linux 5.9+).
func parseWorkerAttestation(status, limits string) (workerAttestation, error) {
	var a workerAttestation
	found := 0
	for _, line := range strings.Split(status, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "NoNewPrivs":
			a.noNewPrivs = value == "1"
			found |= 1
		case "Seccomp":
			n, err := strconv.Atoi(value)
			if err != nil {
				return a, errWorkerAttestation
			}
			a.seccompMode = n
			found |= 2
		case "Seccomp_filters":
			n, err := strconv.Atoi(value)
			if err != nil {
				return a, errWorkerAttestation
			}
			a.seccompFilters = n
			found |= 4
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(limits))
	for scanner.Scan() {
		line := scanner.Text()
		var target *[2]uint64
		var bit int
		var rest string
		switch {
		case strings.HasPrefix(line, "Max cpu time "):
			rest, bit = strings.TrimPrefix(line, "Max cpu time "), 8
		case strings.HasPrefix(line, "Max address space "):
			rest, bit = strings.TrimPrefix(line, "Max address space "), 16
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			return a, errWorkerAttestation
		}
		var pair [2]uint64
		for i := 0; i < 2; i++ {
			if fields[i] == "unlimited" {
				pair[i] = limitUnlimited
				continue
			}
			n, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return a, errWorkerAttestation
			}
			pair[i] = n
		}
		target = &pair
		if bit == 8 {
			a.cpuSoft, a.cpuHard = target[0], target[1]
		} else {
			a.asSoft, a.asHard = target[0], target[1]
		}
		found |= bit
	}
	if found != 31 {
		return a, errWorkerAttestation
	}
	return a, nil
}

// check requires the worker's OWN seccomp filter: inheritedFilters is the
// launcher's count, since Docker/systemd filters are inherited by every child.
func (a workerAttestation) check(inheritedFilters int) error {
	if !a.noNewPrivs || a.seccompMode != 2 || a.seccompFilters <= inheritedFilters {
		return errWorkerAttestation
	}
	if a.cpuHard > launcherCPUCeilingSeconds || a.cpuSoft > a.cpuHard {
		return errWorkerAttestation
	}
	if a.asHard > launcherAddressSpaceCeiling || a.asSoft > a.asHard {
		return errWorkerAttestation
	}
	return nil
}

// statusSeccompFilters reads only Seccomp_filters from /proc/<pid>/status.
func statusSeccompFilters(status string) (int, bool) {
	for _, line := range strings.Split(status, "\n") {
		if value, ok := strings.CutPrefix(line, "Seccomp_filters:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			return n, err == nil && n >= 0
		}
	}
	return 0, false
}

func checkWorkerDescriptors(names []string) error {
	if len(names) != 3 {
		return errWorkerAttestation
	}
	seen := map[string]bool{}
	for _, n := range names {
		if (n != "0" && n != "1" && n != "2") || seen[n] {
			return errWorkerAttestation
		}
		seen[n] = true
	}
	return nil
}

type workerFileFacts struct {
	regular        bool
	mode           uint32 // permission bits
	uid            uint32
	setuid, setgid bool
}

// The worker must be a regular executable owned by root or the service user,
// never set-ID and never group/world writable.
func checkWorkerFileFacts(f workerFileFacts, euid uint32) error {
	if !f.regular || f.setuid || f.setgid || f.mode&0o6000 != 0 || f.mode&0o022 != 0 || f.mode&0o111 == 0 {
		return errWorkerPin
	}
	if f.uid != 0 && f.uid != euid {
		return errWorkerPin
	}
	return nil
}
