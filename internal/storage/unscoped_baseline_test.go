package storage

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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
	return checkUnscopedBaselineFS(root, ref, os.DirFS(root))
}

func checkUnscopedBaselineFS(root, ref string, head fs.FS) error {
	// Resolve the requested base, never replace it with an integration candidate.
	cmd := exec.Command("git", "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	cmd.Dir = root
	resolved, err := cmd.Output()
	if err != nil || strings.TrimSpace(ref) == "" {
		return fmt.Errorf("resolve baseline %q: %v", ref, err)
	}
	ref = strings.TrimSpace(string(resolved))
	base, err := unscopedGitTree(root, ref)
	if err != nil {
		return err
	}
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
	if ref == reviewedP4Source || ref == reviewedMainSource {
		// P4 had already moved this method before adding its pre-CAS scope
		// check, so its own golden does not retain the spelling. Attestation
		// must inspect actual source even when neither current golden names it.
		vocabulary["GetAttachmentByDigest"] = true
	}
	bm, bs, err := collectUnscoped(base, vocabulary)
	if err != nil {
		return fmt.Errorf("base source: %w", err)
	}
	hm, hs, err := collectUnscoped(head, vocabulary)
	if err != nil {
		return fmt.Errorf("head source: %w", err)
	}
	attested, err := reviewedIntegrationSites(root, ref, head, hs, vocabulary)
	if err != nil {
		return err
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
			if i == 1 {
				for key := range attested {
					delete(added, key)
				}
			}
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

// These are the two independently reviewed source commits, NOT a baseline union.
// Exceptions apply only to comparisons against these exact commits. Every future
// PR base uses the original strict source AND golden set comparisons unchanged.
const reviewedP4Source = "014d3d1d5f0a62ef210fab83a10760ef55094c41"
const reviewedMainSource = "cd22b4bdc3b5229621169fe5b214d7ffd12a6015"

func reviewedIntegrationSites(root, base string, head fs.FS, sites, vocabulary debtSet) (debtSet, error) {
	allowed := debtSet{}
	if base != reviewedP4Source && base != reviewedMainSource {
		return allowed, nil
	}
	p4, err := unscopedGitTree(root, reviewedP4Source)
	if err != nil {
		return nil, err
	}
	main, err := unscopedGitTree(root, reviewedMainSource)
	if err != nil {
		return nil, err
	}
	_, p4Sites, err := collectUnscoped(p4, vocabulary)
	if err != nil {
		return nil, err
	}
	_, mainSites, err := collectUnscoped(main, vocabulary)
	if err != nil {
		return nil, err
	}
	for _, spec := range []struct {
		source, file, scope, labels string
	}{
		{reviewedMainSource, "internal/apiserver/live_injector.go", "bridgeLiveInjector.findTaskInstance", "ListAllInstances#1"},
		{reviewedMainSource, "internal/service/resume_with_context.go", "TaskServiceImpl.ResumeTaskWithContext", "MergeMetadata#1"},
		{reviewedMainSource, "internal/service/resume_with_context.go", "TaskServiceImpl.runResumeGate", "ClearDispatchLease#1 GetClaim#1 GetRunner#1 ReleaseClaim#1"},
		{reviewedMainSource, "internal/service/scheduler.go", "SchedulerService.candidateRunners", "ListRunners#1"},
		{reviewedP4Source, "internal/service/attachments.go", "AttachmentServiceImpl.Create", "GetAttachmentByDigest#1"},
	} {
		source := main
		inherited := mainSites
		if spec.source == reviewedP4Source {
			source = p4
			inherited = p4Sites
		}
		keys := parseDebt(spec.file + ":" + spec.scope + " " + spec.labels)
		live := false
		for key := range keys {
			live = live || sites[key]
		}
		if !live {
			continue
		} // Removal never resurrects an allowance.
		// Full enclosing declaration (signature, control flow, receiver, arguments),
		// not a spelling/count or call-only match. Imports used by it are pinned too.
		if err := sameReviewedDeclaration(source, head, spec.file, spec.scope); err != nil {
			return nil, err
		}
		for key := range keys {
			if !inherited[key] {
				return nil, fmt.Errorf("attestation absent from immutable source: %s", key)
			}
			if sites[key] && base != spec.source {
				allowed[key] = true
			}
		}
	}
	// Receiver evidence is independent of the inherited main calls (whose main
	// holders were raw). Pin the P4 bound fields and the interface delegation chain.
	// These declarations confer no additional site/raw/control/package allowance.
	for _, boundary := range []struct{ file, declaration string }{
		{"internal/service/task.go", "TaskServiceImpl.storage"},
		{"internal/service/attachments.go", "AttachmentServiceImpl.storage"},
		{"internal/service/runner_registry.go", "RunnerRegistryServiceImpl.storage"},
		{"internal/service/runner_registry.go", "NewRunnerRegistryService"},
		{"internal/service/runner_registry.go", "RunnerRegistryServiceImpl.ListAllInstances"},
		{"internal/service/runner_registry.go", "RunnerRegistryServiceImpl.ListRunners"},
		{"internal/service/scheduler.go", "SchedulerService.runners"},
		{"internal/service/scheduler.go", "schedulerRunnerRegistry"},
		{"internal/service/scheduler.go", "runnerListResponseAdapter"},
		{"internal/service/scheduler.go", "runnerListResponseAdapter.ListRunners"},
		{"internal/apiserver/goal_steerer.go", "goalInstanceLister"},
	} {
		if err := sameReviewedDeclaration(p4, head, boundary.file, boundary.declaration); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"bridgeLiveInjector.instances", "newBridgeLiveInjector"} {
		if err := sameReviewedDeclaration(main, head, "internal/apiserver/live_injector.go", name); err != nil {
			return nil, err
		}
	}
	if err := sameReviewedDeclaration(main, head, "internal/service/scheduler.go", "NewSchedulerService"); err != nil {
		return nil, err
	}
	return allowed, nil
}

func sameReviewedDeclaration(source, head fs.FS, file, name string) error {
	want, err := reviewedDeclaration(source, file, name)
	if err != nil {
		return err
	}
	got, err := reviewedDeclaration(head, file, name)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("reviewed source/receiver attestation changed: %s:%s", file, name)
	}
	return nil
}

func reviewedDeclaration(tree fs.FS, file, name string) (string, error) {
	data, err := fs.ReadFile(tree, file)
	if err != nil {
		return "", err
	}
	f, err := parser.ParseFile(token.NewFileSet(), file, data, 0)
	if err != nil {
		return "", err
	}
	var found ast.Node
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			key := d.Name.Name
			if receiverName(d) != "" {
				key = receiverName(d) + "." + key
			}
			if key == name {
				found = d
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if ts.Name.Name == name {
					found = ts
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					for _, field := range st.Fields.List {
						for _, id := range field.Names {
							if ts.Name.Name+"."+id.Name == name {
								found = &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{field}}}
							}
						}
					}
				}
			}
		}
	}
	if found == nil {
		return "", fmt.Errorf("missing reviewed declaration %s:%s", file, name)
	}
	var out bytes.Buffer
	if err := format.Node(&out, token.NewFileSet(), found); err != nil {
		return "", err
	}
	// Resolve all import names referenced by this node, so an identically spelled
	// storage.TenantStore from a different import cannot satisfy a field pin.
	used := map[string]bool{}
	ast.Inspect(found, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	for _, imp := range f.Imports {
		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return "", err
		}
		parts := strings.Split(pkg, "/")
		alias := parts[len(parts)-1]
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." || used[alias] {
			fmt.Fprintf(&out, "\nimport %s %q", alias, pkg)
		}
	}
	return out.String(), nil
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
