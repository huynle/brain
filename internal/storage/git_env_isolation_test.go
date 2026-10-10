package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnvHelperFlag marks the re-executed child test binary. Without it the
// helper skips, so selecting GitEnv tests in a normal run never touches git.
const gitEnvHelperFlag = "BRAIN_TEST_GIT_ENV_HELPER"

// gitInDir runs git the way TestUnscopedGitBaseline does: cmd.Dir only, with
// the process environment inherited. That shape committed into the outer
// repository when GIT_DIR/GIT_INDEX_FILE leaked in (Brain quirk hsw9c5yw).
func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Ratchet Test", "-c", "user.email=ratchet@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A temp-repo test that inherits GIT_DIR/GIT_INDEX_FILE (from a git hook,
// `git rebase --exec`, or an exported shell) must still commit only into its
// own t.TempDir(). The child is this test binary, re-executed so the package
// TestMain sees the hazard exactly as a real `go test` invocation would.
func TestGitEnvIsolation_TempRepoCannotWriteOuterRepo(t *testing.T) {
	outer := t.TempDir()
	gitInDir(t, outer, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(outer, "outer.txt"), []byte("outer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, outer, "add", ".")
	gitInDir(t, outer, "commit", "--quiet", "-m", "outer")
	snapshot := func() (head, index string) {
		return gitInDir(t, outer, "rev-parse", "HEAD"), gitInDir(t, outer, "ls-files", "--stage")
	}
	headBefore, indexBefore := snapshot()

	gitDir := filepath.Join(outer, ".git")
	cmd := exec.Command(os.Args[0], "-test.run=^TestGitEnvIsolationHelper$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), gitEnvHelperFlag+"=1", "GIT_DIR="+gitDir, "GIT_INDEX_FILE="+filepath.Join(gitDir, "index"))
	out, runErr := cmd.CombinedOutput()

	headAfter, indexAfter := snapshot()
	if headAfter != headBefore {
		t.Errorf("outer HEAD moved %s -> %s; outer log:\n%s", headBefore, headAfter, gitInDir(t, outer, "log", "--oneline"))
	}
	if indexAfter != indexBefore {
		t.Errorf("outer index changed:\nbefore:\n%s\nafter:\n%s", indexBefore, indexAfter)
	}
	if runErr != nil || !strings.Contains(string(out), "--- PASS: TestGitEnvIsolationHelper") {
		t.Errorf("helper did not pass: %v\n%s", runErr, out)
	}
}

// TestGitEnvIsolationHelper is the child half: the init/add/commit sequence the
// baseline tests use, in its own t.TempDir(), which must own the commit.
func TestGitEnvIsolationHelper(t *testing.T) {
	if os.Getenv(gitEnvHelperFlag) != "1" {
		t.Skip("child process of TestGitEnvIsolation_TempRepoCannotWriteOuterRepo")
	}
	root := t.TempDir()
	gitInDir(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "inner.txt"), []byte("inner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, root, "add", ".")
	gitInDir(t, root, "commit", "--quiet", "-m", "baseline")
	want, err := filepath.EvalSymlinks(filepath.Join(root, ".git"))
	if err != nil {
		t.Fatalf("temp repo has no .git of its own: %v", err)
	}
	got, err := filepath.EvalSymlinks(gitInDir(t, root, "rev-parse", "--absolute-git-dir"))
	if err != nil || got != want {
		t.Fatalf("git operated on %q (%v), want the temp repo %q", got, err, want)
	}
}
