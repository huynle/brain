package scriptexec

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func validLauncherConfig() launcherConfig {
	return launcherConfig{
		Enabled:      true,
		WorkerPath:   "/usr/libexec/brain/brain-script-worker",
		WorkerSHA256: strings.Repeat("ab", 32),
		WallTimeout:  5 * time.Second,
	}
}

func TestWorkerLauncherDefaultOff(t *testing.T) {
	if _, err := newWorkerLauncher(launcherConfig{}); !errors.Is(err, errLauncherDisabled) {
		t.Fatalf("zero config err=%v, want disabled", err)
	}
	cfg := validLauncherConfig()
	cfg.Enabled = false
	if _, err := newWorkerLauncher(cfg); !errors.Is(err, errLauncherDisabled) {
		t.Fatalf("explicit disabled err=%v", err)
	}
}

func TestWorkerLauncherConfigValidation(t *testing.T) {
	mutate := map[string]func(*launcherConfig){
		"relative path":    func(c *launcherConfig) { c.WorkerPath = "brain-script-worker" },
		"empty path":       func(c *launcherConfig) { c.WorkerPath = "" },
		"unclean path":     func(c *launcherConfig) { c.WorkerPath = "/usr/../tmp/w" },
		"short digest":     func(c *launcherConfig) { c.WorkerSHA256 = "abcd" },
		"uppercase digest": func(c *launcherConfig) { c.WorkerSHA256 = strings.Repeat("AB", 32) },
		"zero wall":        func(c *launcherConfig) { c.WallTimeout = 0 },
		"wall over cap":    func(c *launcherConfig) { c.WallTimeout = launcherMaxWall + time.Millisecond },
	}
	for name, m := range mutate {
		cfg := validLauncherConfig()
		m(&cfg)
		if _, err := newWorkerLauncher(cfg); !errors.Is(err, errLauncherConfig) {
			t.Errorf("%s: err=%v, want config refusal", name, err)
		}
	}
}

// LINUX-FIRST-20261006: native macOS (and every non-Linux OS) reports
// unsupported even when explicitly enabled with a valid configuration.
func TestWorkerLauncherUnsupportedOffLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("non-Linux refusal")
	}
	if _, err := newWorkerLauncher(validLauncherConfig()); !errors.Is(err, errLauncherUnsupported) {
		t.Fatalf("err=%v, want unsupported platform", err)
	}
	if got := launcherAvailability(validLauncherConfig()); got != "unsupported_platform" {
		t.Fatalf("availability=%q", got)
	}
}

func TestLauncherAvailabilityReasons(t *testing.T) {
	if got := launcherAvailability(launcherConfig{}); got != "disabled" {
		t.Fatalf("default availability=%q", got)
	}
	bad := validLauncherConfig()
	bad.WallTimeout = 0
	if got := launcherAvailability(bad); got != "misconfigured" {
		t.Fatalf("misconfigured availability=%q", got)
	}
}

// /proc attestation parsing is pure and tested on every host with real kernel
// formats; the live /proc read is exercised by the Linux native fixture.
func TestParseWorkerAttestation(t *testing.T) {
	status := "Name:\tbrain-script-wo\nUmask:\t0022\nState:\tS (sleeping)\nNoNewPrivs:\t1\nSeccomp:\t2\nSeccomp_filters:\t1\n"
	limits := "Limit                     Soft Limit           Hard Limit           Units     \n" +
		"Max cpu time              1                    1                    seconds   \n" +
		"Max file size             unlimited            unlimited            bytes     \n" +
		"Max address space         67108864             67108864             bytes     \n"
	got, err := parseWorkerAttestation(status, limits)
	if err != nil || !got.noNewPrivs || got.seccompMode != 2 || got.seccompFilters != 1 || got.cpuHard != 1 || got.cpuSoft != 1 || got.asHard != 64<<20 || got.asSoft != 64<<20 {
		t.Fatalf("parse=%+v err=%v", got, err)
	}
	if err := got.check(0); err != nil {
		t.Fatalf("sealed attestation refused: %v", err)
	}
	// Docker/systemd filters are inherited and already report Seccomp 2: only a
	// filter count strictly above the launcher's own proves the worker sealed.
	if err := got.check(1); !errors.Is(err, errWorkerAttestation) {
		t.Fatalf("inherited-only filter accepted: %v", err)
	}
	refusals := map[string][2]string{
		"no seccomp":      {strings.Replace(status, "Seccomp:\t2", "Seccomp:\t0", 1), limits},
		"strict seccomp":  {strings.Replace(status, "Seccomp:\t2", "Seccomp:\t1", 1), limits},
		"no nnp":          {strings.Replace(status, "NoNewPrivs:\t1", "NoNewPrivs:\t0", 1), limits},
		"missing seccomp": {strings.Replace(status, "Seccomp:\t2\n", "", 1), limits},
		"missing filters": {strings.Replace(status, "Seccomp_filters:\t1\n", "", 1), limits},
		"unlimited as":    {status, strings.Replace(limits, "67108864             67108864", "unlimited            unlimited", 1)},
		"soft-only as":    {status, strings.Replace(limits, "67108864             67108864", "67108864             unlimited", 1)},
		"as over ceiling": {status, strings.Replace(limits, "67108864             67108864", "67108865             67108865", 1)},
		"unlimited cpu":   {status, strings.Replace(limits, "1                    1                    seconds", "unlimited            unlimited            seconds", 1)},
		"missing as":      {status, strings.Replace(limits, "Max address space         67108864             67108864             bytes     \n", "", 1)},
	}
	for name, in := range refusals {
		a, err := parseWorkerAttestation(in[0], in[1])
		if err == nil {
			err = a.check(0)
		}
		if !errors.Is(err, errWorkerAttestation) {
			t.Errorf("%s: err=%v, want attestation refusal", name, err)
		}
	}
}

func TestCheckWorkerDescriptors(t *testing.T) {
	if err := checkWorkerDescriptors([]string{"0", "1", "2"}); err != nil {
		t.Fatal(err)
	}
	for _, fds := range [][]string{{"0", "1", "2", "3"}, {"0", "1"}, {"0", "1", "2", "2"}, {"0", "1", "x"}} {
		if err := checkWorkerDescriptors(fds); !errors.Is(err, errWorkerAttestation) {
			t.Errorf("%v accepted", fds)
		}
	}
}

func TestCheckWorkerFileMode(t *testing.T) {
	const euid = 1000
	ok := []workerFileFacts{
		{regular: true, mode: 0o755, uid: 0},
		{regular: true, mode: 0o555, uid: euid},
		{regular: true, mode: 0o700, uid: euid},
	}
	for _, f := range ok {
		if err := checkWorkerFileFacts(f, euid); err != nil {
			t.Errorf("%+v refused: %v", f, err)
		}
	}
	bad := map[string]workerFileFacts{
		"not regular":    {regular: false, mode: 0o755, uid: 0},
		"setuid":         {regular: true, mode: 0o4755, uid: 0, setuid: true},
		"setgid":         {regular: true, mode: 0o2755, uid: 0, setgid: true},
		"group writable": {regular: true, mode: 0o775, uid: 0},
		"world writable": {regular: true, mode: 0o757, uid: 0},
		"foreign owner":  {regular: true, mode: 0o755, uid: 4242},
		"not executable": {regular: true, mode: 0o644, uid: 0},
	}
	for name, f := range bad {
		if err := checkWorkerFileFacts(f, euid); !errors.Is(err, errWorkerPin) {
			t.Errorf("%s accepted", name)
		}
	}
}

// Default (non-opt-in) source-limit check: refused before any binary is
// opened, on every OS. Exactly 32KiB passes the size gate and fails later
// for platform/pin reasons instead.
func TestWorkerLauncherSourceLimit(t *testing.T) {
	l := &workerLauncher{cfg: validLauncherConfig()}
	deny := func(Frame, func(Frame) error) error { return errors.New("must not run") }
	for name, source := range map[string]string{
		"over 32KiB": strings.Repeat("a", launcherMaxSourceBytes+1),
		"empty":      "",
		"bad utf8":   "\xff",
	} {
		report, err := l.run(context.Background(), source, deny)
		if !errors.Is(err, errLauncherConfig) || report.started {
			t.Errorf("%s: err=%v report=%+v", name, err, report)
		}
	}
	report, err := l.run(context.Background(), strings.Repeat("a", launcherMaxSourceBytes), deny)
	if errors.Is(err, errLauncherConfig) || err == nil || report.started {
		t.Fatalf("exact 32KiB source refused by the size gate: err=%v", err)
	}
	if launcherMaxSourceBytes != 32<<10 {
		t.Fatal("launcher source limit must equal the worker SOURCE_LIMIT (32KiB)")
	}
}

// The attestation wait keys off Seccomp_filters; missing or malformed values
// must fail closed (kernels older than 5.9 lack the field).
func TestStatusSeccompFilters(t *testing.T) {
	if n, ok := statusSeccompFilters("Name:\tx\nSeccomp:\t2\nSeccomp_filters:\t3\n"); !ok || n != 3 {
		t.Fatalf("got %d %v", n, ok)
	}
	for _, status := range []string{"Seccomp:\t2\n", "Seccomp_filters:\tx\n", "Seccomp_filters:\t-1\n", ""} {
		if _, ok := statusSeccompFilters(status); ok {
			t.Errorf("%q accepted", status)
		}
	}
}
