package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnscopedGitBaseline(t *testing.T) {
	for _, golden := range []bool{true, false} {
		name := "goldens"
		if !golden {
			name = "bootstrap"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct{ name, methods, body, ref, want string }{
				{"coordinated growth", "Old New", "x.Old(); x.New()", "", "methods source"},
				{"site growth", "Old", "x.Old(); x.Old()", "", "sites source"},
				{"shrink", "", "", "", ""},
				{"same count method replacement", "New", "x.New()", "", "methods source"},
				{"same count site replacement", "Old", "x.DB()", "", "sites source"},
				{"missing ref", "Old", "x.Old()", "missing-ref", "resolve baseline"},
				{"empty ref", "Old", "x.Old()", " ", "resolve baseline"},
				{"option ref", "Old", "x.Old()", "--all", "resolve baseline"},
				{"receiver move", "", "x.Old()", "", ""},
				{"method golden growth only", "Old", "x.Old()", "", "methods golden"},
				{"site golden growth only", "Old", "x.Old()", "", "sites golden"},
				{"source growth hidden by golden", "Old", "x.Old(); x.Old()", "", "sites source"},
				{"archive attributes ignored", "Old", "x.Old()", "", ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					git := func(args ...string) string {
						t.Helper()
						cmd := exec.Command("git", append([]string{"-c", "user.name=Ratchet Test", "-c", "user.email=ratchet@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
						cmd.Dir = root
						out, err := cmd.CombinedOutput()
						if err != nil {
							t.Fatalf("git %v: %v\n%s", args, err, out)
						}
						return strings.TrimSpace(string(out))
					}
					write := func(name, text string) {
						t.Helper()
						p := filepath.Join(root, name)
						if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(p, []byte(text), 0644); err != nil {
							t.Fatal(err)
						}
					}
					source := func(methods, body string, goldens bool) {
						text := "package storage\n"
						for _, m := range strings.Fields(methods) {
							text += "func (*StorageLayer) " + m + "() {}\n"
						}
						write("internal/storage/raw.go", text)
						write("internal/service/use.go", "package service\nfunc f() {"+body+"}\n")
						if goldens {
							m, s, err := collectUnscoped(os.DirFS(root), debtSet{"Old": true})
							if err != nil {
								t.Fatal(err)
							}
							write("internal/storage/testdata/storage_unscoped_methods.golden", formatDebt(m))
							write("internal/storage/testdata/storage_unscoped_sites.golden", formatDebt(s))
						}
					}
					git("init", "--quiet")
					source("Old", "x.Old()", golden)
					// Archives would omit the very source needed for bootstrap.
					write(".gitattributes", "internal/service/* export-ignore\n")
					git("add", ".")
					git("commit", "--quiet", "-m", "baseline")
					base := git("rev-parse", "HEAD")
					source(tc.methods, tc.body, true)
					switch tc.name {
					case "method golden growth only":
						write("internal/storage/testdata/storage_unscoped_methods.golden", "New\nOld\n")
					case "site golden growth only":
						write("internal/storage/testdata/storage_unscoped_sites.golden", "internal/service/use.go:f Old#1 Old#2\n")
					case "source growth hidden by golden":
						write("internal/storage/testdata/storage_unscoped_sites.golden", "internal/service/use.go:f Old#1\n")
					}
					if tc.ref != "" {
						base = tc.ref
					}
					err := checkUnscopedBaseline(root, base)
					if tc.want == "" {
						if err != nil {
							t.Fatal(err)
						}
					} else if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("want %q rejection, got %v", tc.want, err)
					}
				})
			}
		})
	}
}
