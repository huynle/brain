package runner

import (
	"fmt"
	"os"
	"testing"
)

// gitRepoRedirectVars make git ignore cmd.Dir / -C and act on another
// repository or index. Inherited from a git hook, `git rebase --exec`, or an
// exported shell, they let temp-repo tests commit into the outer checkout
// (Brain quirk hsw9c5yw). Keep in sync with internal/storage/main_test.go,
// whose TestGitEnvIsolation_TempRepoCannotWriteOuterRepo guards the scrub.
var gitRepoRedirectVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
}

// scrubGitRepoEnv unsets gitRepoRedirectVars for the whole test process, so a
// git child that inherits the environment resolves its repository from its
// own directory.
func scrubGitRepoEnv() error {
	for _, name := range gitRepoRedirectVars {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset %s: %w", name, err)
		}
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := scrubGitRepoEnv(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
