package brainpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestP1Compatibility(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("note"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "note.md"), filepath.Join(root, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "../note.md", "x/../note.md", filepath.Join(root, "note.md"), "\x00", "escape/note.md"} {
		if _, err := ResolveForWrite(root, name); !errors.Is(err, ErrContainment) {
			t.Errorf("%q containment error = %v", name, err)
		}
	}
	p, err := Resolve(root, "alias.md")
	if err != nil || p != filepath.Join(root, "alias.md") {
		t.Errorf("lexical alias = %q, %v", p, err)
	}
	if _, err := Resolve(root, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("existing read missing error = %v", err)
	}
	for _, name := range []string{"new/note.md", "dangling/new.md"} {
		if _, err := ResolveForWrite(root, name); err != nil {
			t.Errorf("write %s: %v", name, err)
		}
	}
}

func fixture(t testing.TB) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "root-sibling")
	for _, dir := range []string{filepath.Join(root, "dir"), outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(root, "dir", "note"), filepath.Join(outside, "note")} {
		if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root, outside
}

func link(t testing.TB, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func TestResolveContainment(t *testing.T) {
	root, outside := fixture(t)
	link(t, outside, filepath.Join(root, "escape"))
	link(t, filepath.Join(outside, "note"), filepath.Join(root, "escape-file"))
	link(t, root, filepath.Join(root, "self"))
	for _, resolve := range []struct {
		name string
		fn   func(string, string) (string, error)
	}{{"read", Resolve}, {"write", ResolveForWrite}} {
		t.Run(resolve.name, func(t *testing.T) {
			for _, name := range []string{"", "\x00", "/etc/passwd", "..", "a/../..", ".", "dir/..", "../root-sibling/note", "escape/note", "escape-file", "self"} {
				t.Run(name, func(t *testing.T) {
					got, err := resolve.fn(root, name)
					if !errors.Is(err, ErrContainment) || got != "" {
						t.Fatalf("got (%q, %v), want empty path and ErrContainment", got, err)
					}
				})
			}
		})
	}
}

func TestResolvePaths(t *testing.T) {
	root, outside := fixture(t)
	link(t, "dir", filepath.Join(root, "inside"))
	link(t, "dir/note", filepath.Join(root, "file-link"))
	link(t, "new/target", filepath.Join(root, "dangling-in"))
	link(t, filepath.Join(outside, "missing"), filepath.Join(root, "dangling-out"))
	link(t, "loop", filepath.Join(root, "loop"))
	alias := filepath.Join(filepath.Dir(root), "alias")
	link(t, root, alias)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join(canonicalRoot, "dir"), filepath.Join(root, "canonical-link"))
	for _, tc := range []struct {
		name, root, path string
		write, fail      bool
	}{
		{"file", root, "dir/note", false, false},
		{"directory", root, "dir", false, false},
		{"intermediate in-root link", root, "inside/note", false, false},
		{"final in-root link", root, "file-link", false, false},
		{"root alias", alias, "dir/note", false, false},
		{"canonical candidate alias root", alias, "canonical-link/note", false, false},
		{"macOS canonical root", canonicalRoot, "inside/note", false, false},
		{"existing write", root, "dir/note", true, false},
		{"missing read", root, "new/nested/note", false, true},
		{"missing write", root, "new/nested/note", true, false},
		{"missing below link", root, "inside/new/note", true, false},
		{"missing root read", filepath.Join(root, "absent/root"), "note", false, true},
		{"missing root write", filepath.Join(alias, "absent/root"), "new/note", true, false},
		{"dangling inside read", root, "dangling-in", false, true},
		{"dangling inside write", root, "dangling-in", true, false},
		{"dangling outside write", root, "dangling-out", true, true},
		{"dangling outside ancestor", root, "dangling-out/new/note", true, true},
		{"file ancestor read", root, "dir/note/child", false, true},
		{"file ancestor write", root, "dir/note/new/child", true, true},
		{"file root", filepath.Join(root, "dir/note"), "child", true, true},
		{"loop read", root, "loop", false, true},
		{"loop write", root, "loop/new", true, true},
		{"empty root", "", "note", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := Resolve
			if tc.write {
				fn = ResolveForWrite
			}
			got, err := fn(tc.root, tc.path)
			if tc.fail {
				if err == nil || got != "" {
					t.Fatalf("got (%q, %v), want error and empty path", got, err)
				}
				return
			}
			want := filepath.Join(tc.root, tc.path)
			if err != nil || got != want {
				t.Fatalf("got (%q, %v), want %q", got, err, want)
			}
		})
	}
}

func TestResolvePreservesDeleteSymlink(t *testing.T) {
	root, _ := fixture(t)
	link(t, "dir/note", filepath.Join(root, "link"))
	path, err := Resolve(root, "link")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir/note")); err != nil {
		t.Fatalf("target removed: %v", err)
	}
}

func TestResolveForWriteRejectsAmbiguousDanglingTarget(t *testing.T) {
	root, outside := fixture(t)
	// Lexically, escape/../missing is inside root; on the filesystem it is
	// outside root because escape resolves to its sibling first.
	link(t, outside, filepath.Join(root, "escape"))
	link(t, "escape/../missing", filepath.Join(root, "ambiguous"))
	got, err := ResolveForWrite(root, "ambiguous")
	if got != "" || !errors.Is(err, ErrContainment) {
		t.Fatalf("got (%q, %v), want empty path and ErrContainment", got, err)
	}
}

func BenchmarkResolve(b *testing.B) {
	root, _ := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Resolve(root, "dir/note"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveForWriteMissing(b *testing.B) {
	root, _ := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ResolveForWrite(root, "new/nested/note"); err != nil {
			b.Fatal(err)
		}
	}
}
