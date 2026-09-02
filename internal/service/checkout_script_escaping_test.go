package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// Every caller-supplied value baked into the generated bash must be escaped
// for the context it lands in. merge_target_branch used to go in raw while
// the other three were hardened, so a quote in it closed the shell literal
// and the remainder ran as code on the runner host.
func TestCheckoutScript_CallerValuesCannotEscapeTheirShellLiteral(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pwned")

	svc, _, brainDir := newTestTaskService(t)
	_, err := svc.CheckoutFeature(context.Background(), "brain", "feat-inj",
		&types.FeatureCheckoutOptions{
			CheckoutMode: "simple",
			// Closes TARGET_BRANCH's literal and appends a command.
			MergeTargetBranch: "main'; touch " + marker + " ; echo '",
			ExecutionBranch:   "br'; touch " + marker + ".b ; echo '",
		})
	if err != nil {
		t.Fatalf("CheckoutFeature: %v", err)
	}

	raw := readCheckoutTaskFile(t, brainDir, "brain")
	script := extractDirectPrompt(t, raw)

	// Running it must not execute the injected command. It will fail (no git
	// repo here) — that is fine; what matters is the marker never appears.
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = t.TempDir()
	out, _ := cmd.CombinedOutput()

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("INJECTION: target-branch payload executed.\nscript:\n%s\noutput:\n%s", script, out)
	}
	if _, err := os.Stat(marker + ".b"); err == nil {
		t.Fatalf("INJECTION: execution-branch payload executed.\nscript:\n%s", script)
	}
}

// Branch names git accepts must reach git unchanged. They used to be filtered
// down to [A-Za-z0-9._/-]; the script's "branch no longer exists" guard then
// matched the mangled name and exited 0, marking the task completed having
// merged nothing.
func TestCheckoutScript_LegalBranchNamesSurviveVerbatim(t *testing.T) {
	for _, branch := range []string{
		"release/v1.0+rc1",
		"fix/issue#42",
		"feature/über-fix",
		"wip@home",
	} {
		t.Run(branch, func(t *testing.T) {
			svc, _, brainDir := newTestTaskService(t)
			_, err := svc.CheckoutFeature(context.Background(), "brain", "feat-"+branch,
				&types.FeatureCheckoutOptions{
					CheckoutMode:    "simple",
					ExecutionBranch: branch,
				})
			if err != nil {
				t.Fatalf("CheckoutFeature: %v", err)
			}
			script := extractDirectPrompt(t, readCheckoutTaskFile(t, brainDir, "brain"))
			want := "SOURCE_BRANCH='" + branch + "'"
			if !strings.Contains(script, want) {
				t.Errorf("branch was altered.\nwant line: %s\ngot script:\n%s", want, script)
			}
		})
	}
}

// extractDirectPrompt pulls the script back out of the task's YAML literal
// block, undoing the two-space block indent frontmatter.Serialize applies.
func extractDirectPrompt(t *testing.T, raw string) string {
	t.Helper()
	idx := strings.Index(raw, "direct_prompt: |")
	if idx < 0 {
		t.Fatalf("no direct_prompt block in:\n%s", raw)
	}
	var out []string
	for _, line := range strings.Split(raw[idx:], "\n")[1:] {
		if line != "" && !strings.HasPrefix(line, "  ") {
			break
		}
		out = append(out, strings.TrimPrefix(line, "  "))
	}
	return strings.Join(out, "\n")
}
