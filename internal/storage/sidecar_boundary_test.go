package storage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// Sidecars are two explicit single-install implementations, not a registry or
// a substitute for TenantStore. No original raw/control/package allowance grows.
func sidecarSites(tree fs.FS) (map[string]int, error) {
	out := map[string]int{}
	err := fs.WalkDir(tree, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "node_modules", "vendor", "testdata", "dist":
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
		f, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			alias := path.Base(p)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			if strings.HasSuffix(p, "/assistantjobs") || strings.HasSuffix(p, "/phonepush") || p == "database/sql" {
				if alias == "." {
					return fmt.Errorf("dot imported sidecar/SQL: %s", name)
				}
				imports[alias] = p
			}
		}
		isSidecar := strings.HasPrefix(name, "internal/assistantjobs/") || strings.HasPrefix(name, "internal/phonepush/")
		for _, decl := range f.Decls {
			fnName := "package"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fnName = fn.Name.Name
				if isSidecar && fn.Recv == nil && fn.Name.IsExported() {
					out[name+":"+fnName+":export"]++
				}
				if isSidecar && fn.Name.IsExported() {
					var escape bool
					ast.Inspect(fn.Type, func(n ast.Node) bool {
						if sel, ok := n.(*ast.SelectorExpr); ok {
							if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] == "database/sql" {
								escape = true
							}
						}
						return true
					})
					if escape {
						return fmt.Errorf("sidecar SQL signature escape: %s:%s", name, fnName)
					}
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg := imports[id.Name]
				if pkg == "github.com/huynle/brain-api/internal/assistantjobs" && sel.Sel.Name == "Open" || pkg == "github.com/huynle/brain-api/internal/phonepush" && sel.Sel.Name == "OpenPhonePush" || isSidecar && pkg == "database/sql" && sel.Sel.Name == "Open" {
					out[name+":"+fnName+":"+path.Base(pkg)+"."+sel.Sel.Name]++
				}
				return true
			})
		}
		return nil
	})
	return out, err
}

func TestSingleModeSidecarBoundary(t *testing.T) {
	got, err := sidecarSites(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"internal/assistantjobs/store.go:Open:export":                             1,
		"internal/assistantjobs/store.go:NewID:export":                            1,
		"internal/assistantjobs/store.go:Open:sql.Open":                           1,
		"internal/phonepush/store.go:OpenPhonePush:export":                        1,
		"internal/phonepush/store.go:PushID:export":                               1,
		"internal/phonepush/store.go:ValidatePushDevice:export":                   1,
		"internal/phonepush/store.go:OpenPhonePush:sql.Open":                      1,
		"internal/api/assistant_jobs.go:StartConversationJobs:assistantjobs.Open": 1,
		"internal/apiserver/server.go:buildHTTPHandler:phonepush.OpenPhonePush":   1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sidecar constructor/export inventory changed:\n got %v\nwant %v", got, want)
	}
}

func TestSidecarBoundaryDetectsNewConstructorAndRawEscape(t *testing.T) {
	for _, body := range []string{
		`package assistantjobs; import "database/sql"; func Leak() *sql.DB { return nil }`,
		`package assistantjobs; import "database/sql"; func Extra(){ sql.Open("sqlite", "x") }`,
	} {
		got, err := sidecarSites(fstest.MapFS{"internal/assistantjobs/new.go": &fstest.MapFile{Data: []byte(body)}})
		if err == nil && len(got) == 0 {
			t.Fatal("unreviewed sidecar escaped inventory")
		}
	}
}
