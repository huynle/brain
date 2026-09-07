package storage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These are migration inventories, not compile-time tenant isolation. The method
// list guards definitions, not new calls to existing methods; the separate site
// list is a conservative spelling/count inventory, not type or data-flow analysis.
// Neither detects missing predicates on correctly typed handles, raw SQL added
// inside approved methods, or new unscoped tables. P4.10 must remove TenantStore's
// StorageLayer embedding to establish the compile-time API boundary.
type debtSet map[string]bool

func TestProductionUnscopedStorage(t *testing.T) {
	readGolden := func(name string) debtSet {
		t.Helper()
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		set := parseDebt(string(data))
		if formatDebt(set) != string(data) {
			t.Fatalf("%s must be canonical (sorted, unique, newline terminated)", name)
		}
		return set
	}
	methods := readGolden("storage_unscoped_methods.golden")
	sites := readGolden("storage_unscoped_sites.golden")
	vocabulary := debtSet{}
	for name := range methods {
		vocabulary[name] = true
	}
	// Retain selector spellings even after a raw method moves receivers. Do not
	// erase still-live references merely because the definitions golden shrank.
	for key := range sites {
		_, label, _ := strings.Cut(key, " ")
		name, _, _ := strings.Cut(label, "#")
		vocabulary[name] = true
	}
	actualMethods, actualSites, err := collectUnscoped(os.DirFS("../.."), vocabulary)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		actual, golden debtSet
	}{
		{"storage_unscoped_methods.golden", actualMethods, methods},
		{"storage_unscoped_sites.golden", actualSites, sites},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added, stale := compareDebt(tc.actual, tc.golden)
			if len(added)+len(stale) != 0 {
				t.Errorf("%s: actual count=%d, golden count=%d; additions forbidden; remove stale allowances\nADDED:\n%sSTALE:\n%s", tc.name, len(tc.actual), len(tc.golden), formatDebt(added), formatDebt(stale))
			}
		})
	}
}

func collectUnscoped(tree fs.FS, vocabulary debtSet) (debtSet, debtSet, error) {
	files := map[string]*ast.File{}
	err := fs.WalkDir(tree, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name == ".claude/worktrees" {
				return fs.SkipDir
			}
			switch d.Name() {
			case ".git", ".worktrees", "vendor", "node_modules", "testdata", "storagetest":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := fs.ReadFile(tree, name)
		if err != nil {
			return err
		}
		// Parse every production file, independent of host GOOS or build tags.
		f, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			return err
		}
		files[name] = f
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	methods, sites, names := debtSet{}, debtSet{}, debtSet{"DB": true}
	for name := range vocabulary {
		names[name] = true
	}
	for name, file := range files {
		if path.Dir(name) != "internal/storage" {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && receiverName(fn) == "StorageLayer" {
				methods[fn.Name.Name] = true
				names[fn.Name.Name] = true
			}
		}
	}
	// Close is ubiquitous across unrelated resources. Ownership policy audits
	// raw-owner Close; this syntax-only inventory deliberately omits other Close.
	delete(names, "Close")
	for name, file := range files {
		if path.Dir(name) == "internal/storage" {
			continue // Definitions/delegation within storage are not external debt.
		}
		sqlNames := map[string]bool{}
		for _, imp := range file.Imports {
			pkg, _ := strconv.Unquote(imp.Path.Value)
			if pkg == "database/sql" {
				alias := "sql"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				sqlNames[alias] = true
			}
		}
		counts := map[string]int{}
		add := func(scope, label string) {
			key := name + ":" + scope + " " + label
			counts[key]++
			sites[fmt.Sprintf("%s#%d", key, counts[key])] = true
		}
		for _, decl := range file.Decls {
			scope := "package"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				scope = fn.Name.Name
				if recv := receiverName(fn); recv != "" {
					scope = recv + "." + scope
				}
				// Inventory explicit DB-returning wrappers even if not named DB.
				if fn.Type.Results != nil {
					ast.Inspect(fn.Type.Results, func(n ast.Node) bool {
						switch n := n.(type) {
						case *ast.SelectorExpr:
							if base, ok := n.X.(*ast.Ident); ok && sqlNames[base.Name] && n.Sel.Name == "DB" {
								add(scope, "db-return")
							}
						case *ast.Ident:
							if sqlNames["."] && n.Name == "DB" {
								add(scope, "db-return")
							}
						}
						return true
					})
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if s, ok := n.(*ast.SelectorExpr); ok && names[s.Sel.Name] {
					// Include type/field selectors too: import spellings can
					// be shadowed, and syntax does not prove a receiver type.
					add(scope, s.Sel.Name)
				}
				return true
			})
		}
	}
	return methods, sites, nil
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	var expr ast.Expr = fn.Recv.List[0].Type
	for {
		switch n := expr.(type) {
		case *ast.StarExpr:
			expr = n.X
		case *ast.ParenExpr:
			expr = n.X
		case *ast.Ident:
			return n.Name
		default:
			return ""
		}
	}
}

// A line is a method name, or a stable file:receiver.function scope followed
// by selector#occurrence tokens. No line numbers: formatting does not add debt.
func parseDebt(text string) debtSet {
	set := debtSet{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 1 {
			set[fields[0]] = true
		} else if len(fields) > 1 {
			for _, label := range fields[1:] {
				set[fields[0]+" "+label] = true
			}
		}
	}
	return set
}

func formatDebt(set debtSet) string {
	groups := map[string][]string{}
	for key := range set {
		scope, label, _ := strings.Cut(key, " ")
		groups[scope] = append(groups[scope], label)
	}
	var scopes []string
	for scope := range groups {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	var out strings.Builder
	for _, scope := range scopes {
		labels := groups[scope]
		sort.Strings(labels)
		out.WriteString(strings.TrimSpace(scope+" "+strings.Join(labels, " ")) + "\n")
	}
	return out.String()
}

// Shared by checkout-vs-golden and base-vs-head checks. Compare
// identities, not just counts: a same-size replacement is still an addition.
func compareDebt(actual, golden debtSet) (debtSet, debtSet) {
	added, stale := debtSet{}, debtSet{}
	for key := range actual {
		if !golden[key] {
			added[key] = true
		}
	}
	for key := range golden {
		if !actual[key] {
			stale[key] = true
		}
	}
	return added, stale
}
