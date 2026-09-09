package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func TestProductionUnscopedStorageBaseline(t *testing.T) {
	ref, requested := os.LookupEnv("BRAIN_STORAGE_RATCHET_BASE")
	if !requested {
		if os.Getenv("GITHUB_EVENT_NAME") == "pull_request" {
			t.Fatal("BRAIN_STORAGE_RATCHET_BASE required for pull requests")
		}
		t.Skip("no BRAIN_STORAGE_RATCHET_BASE: historical check only skipped; current goldens checked separately")
	}
	if err := checkUnscopedBaseline("../..", ref); err != nil {
		t.Fatal(err)
	}
}

func checkUnscopedBaseline(root, ref string) error {
	base, err := unscopedGitTree(root, ref)
	if err != nil {
		return err
	}
	head := os.DirFS(root)
	bm, _, err := collectUnscoped(base, nil)
	if err != nil {
		return fmt.Errorf("base source: %w", err)
	}
	hm, _, err := collectUnscoped(head, nil)
	if err != nil {
		return fmt.Errorf("head source: %w", err)
	}
	vocabulary := debtSet{}
	for _, set := range []debtSet{bm, hm} {
		for key := range set {
			vocabulary[key] = true
		}
	}
	names := []string{"methods", "sites"}
	bg, hg := make([]debtSet, 2), make([]debtSet, 2)
	for i, name := range names {
		for j, tree := range []fs.FS{base, head} {
			file := "internal/storage/testdata/storage_unscoped_" + name + ".golden"
			data, err := fs.ReadFile(tree, file)
			if j == 0 && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("%s golden (base=%t): %w", name, j == 0, err)
			}
			set := parseDebt(string(data))
			if formatDebt(set) != string(data) {
				return fmt.Errorf("%s golden (base=%t) is not canonical", name, j == 0)
			}
			if j == 0 {
				bg[i] = set
			} else {
				hg[i] = set
			}
			for key := range set {
				if i == 0 {
					vocabulary[key] = true
				} else {
					_, label, _ := strings.Cut(key, " ")
					selector, _, _ := strings.Cut(label, "#")
					vocabulary[selector] = true
				}
			}
		}
	}
	// One vocabulary for both scans: receiver migrations must not manufacture
	// additions or erase references to formerly raw methods.
	bm, bs, err := collectUnscoped(base, vocabulary)
	if err != nil {
		return fmt.Errorf("base source: %w", err)
	}
	hm, hs, err := collectUnscoped(head, vocabulary)
	if err != nil {
		return fmt.Errorf("head source: %w", err)
	}
	var failures []string
	for i, pair := range [][2]debtSet{{bm, hm}, {bs, hs}} {
		if bg[i] == nil {
			bg[i] = pair[0]
		} // Bootstrap only from actual base bytes.
		for _, check := range []struct {
			label            string
			actual, baseline debtSet
		}{
			{"source", pair[1], pair[0]}, {"golden", hg[i], bg[i]},
		} {
			added, _ := compareDebt(check.actual, check.baseline)
			if len(added) > 0 {
				failures = append(failures, fmt.Sprintf("%s %s: head=%d base=%d; additions forbidden:\n%s", names[i], check.label, len(check.actual), len(check.baseline), formatDebt(added)))
			}
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// Read object bytes, not an archive/checkout: no export-ignore/export-subst,
// filters, hooks, symlink traversal, or writes to the worktree. NUL-delimited
// tree records preserve unusual filenames. Resolve once before reading objects.
func unscopedGitTree(root, ref string) (fs.FS, error) {
	git := func(input []byte, args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Stdin = bytes.NewReader(input)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
		}
		return out, nil
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("resolve baseline: empty ref")
	}
	sha, err := git(nil, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve baseline: %w", err)
	}
	listing, err := git(nil, "ls-tree", "-r", "-z", "--full-tree", strings.TrimSpace(string(sha)))
	if err != nil {
		return nil, err
	}
	var paths []string
	var objects strings.Builder
	for _, record := range strings.Split(string(listing), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || !fs.ValidPath(name) {
			return nil, fmt.Errorf("invalid git tree record %q", record)
		}
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".golden") {
			continue
		}
		if fields[0] != "100644" && fields[0] != "100755" {
			return nil, fmt.Errorf("non-regular baseline file %q", name)
		}
		paths = append(paths, name)
		objects.WriteString(fields[2] + "\n")
	}
	data, err := git([]byte(objects.String()), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	tree := fstest.MapFS{}
	for _, name := range paths {
		header, rest, ok := bytes.Cut(data, []byte("\n"))
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 3 || fields[1] != "blob" {
			return nil, fmt.Errorf("invalid blob header for %q", name)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size >= len(rest) || rest[size] != '\n' {
			return nil, fmt.Errorf("invalid blob size for %q", name)
		}
		tree[name] = &fstest.MapFile{Data: rest[:size], Mode: 0644}
		data = rest[size+1:]
	}
	if len(data) != 0 {
		return nil, errors.New("unexpected trailing git object data")
	}
	return tree, nil
}
