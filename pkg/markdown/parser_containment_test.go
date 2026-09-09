package markdown

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/brainpath"
)

func TestParseFile_RejectsUncontainedPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "brain")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.md")
	if err := os.WriteFile(outside, []byte("---\ntitle: External secret\n---\nsecret body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.md")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.md", "escape.md", outside} {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseFile(name, root)
			if !errors.Is(err, brainpath.ErrContainment) {
				t.Errorf("expected containment error, got %v", err)
			}
			if parsed != nil {
				t.Error("rejected path returned parsed content")
			}
		})
	}
}
