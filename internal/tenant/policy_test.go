package tenant

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const tenantImport = "github.com/huynle/brain-api/internal/tenant"

// This is a CI review convention, not a type-level authorization guarantee.
// It scans even build-tagged files. Parsing still requires exact-path review;
// Into and BindAuthorized are allowed only at the trusted boundaries below.
func tenantPolicy(path, source string) ([]string, error) {
	if strings.HasSuffix(path, "_test.go") {
		return nil, nil
	}
	var src any
	if source != "" {
		src = source
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("..", "..", filepath.FromSlash(path)), src, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	aliases := map[string]bool{}
	for _, imp := range f.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if value != tenantImport {
			continue
		}
		name := "tenant"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			violations = append(violations, path+": dot tenant import forbidden")
		}
		aliases[name] = true
	}
	if path == "internal/tenant/id.go" {
		return violations, nil
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	bindingAllowed := strings.HasSuffix(path, ".go") && (path == "internal/api/tenant_middleware.go" ||
		dir == "internal/tenant" || dir == "internal/apiserver" || dir == "internal/mcpserver")
	restricted := func(name string) bool {
		return name == "Parse" || name == "MustParse" || ((name == "Into" || name == "BindAuthorized") && !bindingAllowed)
	}
	declarations := map[*ast.Ident]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			declarations[fn.Name] = true
		}
	}
	samePackage := filepath.ToSlash(filepath.Dir(path)) == "internal/tenant" && f.Name.Name == "tenant"
	selectorNames := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			selectorNames[n.Sel] = true
			if base, ok := n.X.(*ast.Ident); ok && base.Obj == nil && aliases[base.Name] && restricted(n.Sel.Name) {
				violations = append(violations, fmt.Sprintf("%s:%d: tenant.%s requires review", path, fset.Position(n.Pos()).Line, n.Sel.Name))
			}
			// Visit X (which could contain a call); Sel is not an unqualified name.
			return true
		case *ast.Ident:
			if samePackage && restricted(n.Name) && !declarations[n] && !selectorNames[n] && (n.Obj == nil || n.Obj.Kind == ast.Fun) {
				violations = append(violations, fmt.Sprintf("%s:%d: %s requires review", path, fset.Position(n.Pos()).Line, n.Name))
			}
		}
		return true
	})
	return violations, nil
}

func TestTenantPolicyChecker(t *testing.T) {
	for _, tc := range []struct {
		name, path, source string
		count              int
	}{
		{"default", "internal/api/new.go", `package api; import "` + tenantImport + `"; var f = tenant.Parse`, 1},
		{"alias", "internal/api/new.go", `package api; import scope "` + tenantImport + `"; var f = scope.MustParse`, 1},
		{"into-reference", "internal/api/new.go", `package api; import scope "` + tenantImport + `"; var f = scope.Into`, 1},
		{"call", "internal/api/new.go", `package api; import t "` + tenantImport + `"; var _, _ = t.Parse("x")`, 1},
		{"dot", "internal/tenant/id.go", `package tenant; import . "` + tenantImport + `"`, 1},
		{"reviewed", "internal/tenant/id.go", `package tenant; func f() { Parse("x") }; var p = MustParse`, 0},
		{"same-package", "internal/tenant/extra.go", `package tenant; var p = Parse`, 1},
		{"not-directory-allowlist", "internal/tenant/extra.go", `package tenant; func f() { MustParse("x") }`, 1},
		{"test-exempt", "internal/api/new_test.go", `package api; import t "` + tenantImport + `"; var f = t.Parse`, 0},
		{"unrelated", "internal/api/new.go", `package api; import tenant "other/package"; var f = tenant.Parse`, 0},
		{"same-package-unrelated-selector", "internal/tenant/extra.go", `package tenant; import "other/package"; var f = other.Parse`, 0},
		{"safe", "internal/api/new.go", `package api; import t "` + tenantImport + `"; var id t.ID`, 0},
		{"shadow", "internal/api/new.go", `package api; import t "` + tenantImport + `"; func f(t struct{ Parse func() }) { t.Parse() }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tenantPolicy(tc.path, tc.source)
			if err != nil || len(got) != tc.count {
				t.Fatalf("violations = %v, %v; want %d", got, err, tc.count)
			}
		})
	}
	if _, err := tenantPolicy("bad.go", "not go"); err == nil {
		t.Error("syntax errors must fail policy")
	}
}

func TestProductionTenantCallsitePolicy(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Empty source asks the parser to read the real file.
		violations, err := tenantPolicy(filepath.ToSlash(rel), "")
		if err != nil {
			return err
		}
		for _, v := range violations {
			t.Error(v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type tenantTypeImporter struct {
	actual   *types.Package
	fallback types.Importer
}

func (i tenantTypeImporter) Import(path string) (*types.Package, error) {
	if path == tenantImport {
		return i.actual, nil
	}
	return i.fallback.Import(path)
}

func TestIDCannotBeConvertedFromString(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	cfg := types.Config{Importer: importer.Default()}
	actual, err := cfg.Check(tenantImport, fset, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Importer = tenantTypeImporter{actual, importer.Default()}
	for _, tc := range []struct {
		expr  string
		fails bool
	}{{`tenant.MustParse("x")`, false}, {`tenant.ID("x")`, true}, {`tenant.ID{v:"x"}`, true}} {
		f, err := parser.ParseFile(fset, "consumer.go", `package consumer; import "`+tenantImport+`"; var _ = `+tc.expr, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = cfg.Check("consumer", fset, []*ast.File{f}, nil)
		if (err != nil) != tc.fails {
			t.Fatalf("%s: %v; want failure=%v", tc.expr, err, tc.fails)
		}
		if tc.expr == `tenant.ID("x")` && !strings.Contains(err.Error(), "cannot convert") {
			t.Fatalf("unexpected type error: %v", err)
		}
	}
}
