package scriptexec

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const scriptWorkerDir = "../../runtime/script-worker/"

// scriptWorkerRelease is the committed reproducible-build record for the
// sealed QuickJS worker. It pins inputs and expected outputs; it activates
// nothing.
type scriptWorkerRelease struct {
	SourceURL       string            `json:"source_url"`
	SourceSHA256    string            `json:"source_sha256"`
	CompilerImage   string            `json:"compiler_image"`
	CompilerVersion string            `json:"compiler_version"`
	Entry           string            `json:"entry"`
	InstallPath     string            `json:"install_path"`
	Outputs         map[string]string `json:"outputs"`
}

func loadScriptWorkerRelease(t *testing.T) scriptWorkerRelease {
	t.Helper()
	data, err := os.ReadFile(scriptWorkerDir + "release.json")
	if err != nil {
		t.Fatal(err)
	}
	var r scriptWorkerRelease
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("release.json: %v", err)
	}
	return r
}

func TestScriptWorkerReleaseRecord(t *testing.T) {
	r := loadScriptWorkerRelease(t)
	if r.SourceURL != "https://bellard.org/quickjs/quickjs-2026-06-04.tar.xz" || r.SourceSHA256 != "b376e839b322978313d929fd20663b11ba58b75df5a46c126dd19ea2fa70ad2a" {
		t.Fatalf("source pin changed: %+v", r)
	}
	if !strings.HasPrefix(r.CompilerImage, "sha256:") || !lowerHexSHA256(strings.TrimPrefix(r.CompilerImage, "sha256:")) || r.CompilerVersion == "" {
		t.Fatalf("compiler pin: %+v", r)
	}
	if r.Entry != "worker.c" || r.InstallPath != "/usr/libexec/brain/brain-script-worker" {
		t.Fatalf("entry/install path: %+v", r)
	}
	if len(r.Outputs) == 0 {
		t.Fatal("no recorded output digests")
	}
	for platform, digest := range r.Outputs {
		if (platform != "linux/arm64" && platform != "linux/amd64") || !lowerHexSHA256(digest) {
			t.Errorf("output %s=%s", platform, digest)
		}
	}
	// The launcher's install-path default must validate as a launcher config.
	cfg := launcherConfig{Enabled: true, WorkerPath: r.InstallPath, WorkerSHA256: r.Outputs["linux/arm64"], WallTimeout: launcherMaxWall}
	if err := cfg.validate(); err != nil {
		t.Fatalf("recorded install path/pin not a valid launcher config: %v", err)
	}
}

// The shipped worker and the confinement probe share ONE seal implementation;
// the release recipe must keep its hardening flags and the worker must not be
// a testdata fixture.
func TestScriptWorkerReleaseSources(t *testing.T) {
	worker, err := os.ReadFile(scriptWorkerDir + "worker.c")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(worker, []byte(`#include "seal.h"`)) || bytes.Contains(worker, []byte(`probe.c`)) {
		t.Fatal("worker must use the shared seal.h, not the test probe")
	}
	probe, err := os.ReadFile("testdata/quickjs_probe.c")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(probe, []byte(`#include "seal.h"`)) || bytes.Contains(probe, []byte("static int seal(void)")) {
		t.Fatal("probe must exercise the shipped seal.h, not a private copy")
	}
	seal, err := os.ReadFile(scriptWorkerDir + "seal.h")
	if err != nil || !bytes.Contains(seal, []byte("static int seal(void)")) || !bytes.Contains(seal, []byte("SECCOMP_FILTER_FLAG_TSYNC")) {
		t.Fatalf("seal.h: %v", err)
	}
	recipe, err := os.ReadFile(scriptWorkerDir + "build.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"SOURCE_DATE_EPOCH=", "-D_FORTIFY_SOURCE=3", "-fstack-protector-strong", "-fPIE -pie", "-z,relro,-z,now,-z,noexecstack", "-ffile-prefix-map="} {
		if !bytes.Contains(recipe, []byte(flag)) {
			t.Errorf("release recipe lost %q", flag)
		}
	}
	for _, gone := range []string{"testdata/quickjs_worker.c", "testdata/build-quickjs-probe.sh"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s must move to runtime/script-worker", gone)
		}
	}
}
