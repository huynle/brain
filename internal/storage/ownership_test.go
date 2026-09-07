package storage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This is an ownership/call-site policy, not the P3.7 method ratchet and not
// isolation. In particular, TenantStore still promotes DB until P4.10.
func storageOwnershipPolicy(path string, source any) ([]string, error) {
	if strings.HasSuffix(path, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	imports := map[string]string{}
	for _, imp := range f.Imports {
		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(pkg)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if strings.HasPrefix(pkg, "github.com/huynle/brain-api/internal/") {
			imports[name] = pkg
			if name == "." && (strings.HasSuffix(pkg, "/storage") || strings.HasSuffix(pkg, "/auth")) {
				violations = append(violations, "dot storage/auth import forbidden")
			}
			if strings.HasSuffix(pkg, "/storagetest") {
				violations = append(violations, "production imports test fixture")
			}
			if strings.HasSuffix(pkg, "/tokens") && path != "cmd/brain/commands/token.go" {
				violations = append(violations, "offline token administration may only be imported by the token CLI")
			}
		}
	}
	ownerFunction := map[string]string{
		"internal/apiserver/storage.go": "openSingleModeStorage",
		"internal/tokens/direct.go":     "openDatabase",
		"internal/doctor/checks.go":     "loadAttachmentDigestChecksFromDatabase",
	}[path]
	storageImpl := filepath.ToSlash(filepath.Dir(path)) == "internal/storage"
	fixture := path == "internal/storage/storagetest/store.go"
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	function := func(n ast.Node) string {
		for p := n; p != nil; p = parents[p] {
			if fn, ok := p.(*ast.FuncDecl); ok {
				return fn.Name.Name
			}
		}
		return ""
	}
	ast.Inspect(f, func(n ast.Node) bool {
		// An inferred owner is just as dangerous as a declared raw pointer. Only
		// factory/lifecycle calls may consume it; no argument passing, aliasing,
		// returned owner, or raw workload queries even in the composition file.
		if id, ok := n.(*ast.Ident); ok && id.Name == "owner" && ownerFunction != "" {
			allowed := false
			switch p := parents[id].(type) {
			case *ast.AssignStmt:
				allowed = p.Tok == token.DEFINE && len(p.Lhs) > 0 && p.Lhs[0] == id
			case *ast.SelectorExpr:
				if p.X == id {
					switch p.Sel.Name {
					case "Close", "ForTenant", "Control", "SingleModeTokens":
						allowed = true
					}
				}
			}
			if !allowed || function(n) != ownerFunction {
				violations = append(violations, "raw owner may only initialize views or close in its audited function")
			}
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == "AuthenticateLocalDatabaseOwner" && f.Name.Name == "auth" && filepath.ToSlash(filepath.Dir(path)) == "internal/auth" {
			fn, declaration := parents[id].(*ast.FuncDecl)
			if !declaration || fn.Name != id || path != "internal/auth/local_owner.go" {
				violations = append(violations, "unqualified local-owner authentication requires review")
			}
		}
		s, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, isIdent := s.X.(*ast.Ident)
		pkg := ""
		if isIdent && base.Obj == nil {
			pkg = imports[base.Name]
		}
		deny := false
		owner := ownerFunction != "" && function(s) == ownerFunction
		if strings.HasSuffix(pkg, "/storage") {
			switch s.Sel.Name {
			case "StorageLayer":
				deny = !fixture // Even composition must not retain/return raw holders.
			case "New", "NewWithDB":
				deny = !owner && !fixture
				if owner {
					call, called := parents[s].(*ast.CallExpr)
					assignment, assigned := parents[call].(*ast.AssignStmt)
					if !called || !assigned || len(assignment.Lhs) == 0 {
						deny = true
					} else if id, ok := assignment.Lhs[0].(*ast.Ident); !ok || id.Name != "owner" {
						deny = true
					}
				}
			}
		}
		// Also catches promoted raw-pointer access and method-value references.
		if s.Sel.Name == "StorageLayer" && pkg == "" {
			deny = !storageImpl
		}
		if s.Sel.Name == "AuthenticateLocalDatabaseOwner" {
			deny = !owner || path == "internal/doctor/checks.go"
		}
		if s.Sel.Name == "TenantRegistry" || s.Sel.Name == "TokenAdmin" || s.Sel.Name == "SingleModeTokens" {
			deny = !storageImpl && (!owner || path == "internal/doctor/checks.go")
		}
		if deny {
			violations = append(violations, fmt.Sprintf("%s:%d: %s requires an audited ownership seam", path, fset.Position(s.Pos()).Line, s.Sel.Name))
		}
		return true
	})
	return violations, nil
}

func TestStorageOwnershipPolicyChecker(t *testing.T) {
	for _, tc := range []struct {
		name, path, source string
		denied             bool
	}{
		{"holder", "internal/service/new.go", `package service; import s "github.com/huynle/brain-api/internal/storage"; type X struct { store *s.StorageLayer }`, true},
		{"alias", "internal/api/new.go", `package api; import s "github.com/huynle/brain-api/internal/storage"; type X = s.StorageLayer`, true},
		{"open reference", "internal/api/new.go", `package api; import s "github.com/huynle/brain-api/internal/storage"; var open = s.New`, true},
		{"dot", "internal/api/new.go", `package api; import . "github.com/huynle/brain-api/internal/storage"`, true},
		{"escape", "internal/service/new.go", `package service; func f(s any) { _ = s.StorageLayer }`, true},
		{"operator", "internal/api/new.go", `package api; func f(c any) { _ = c.TokenAdmin }`, true},
		{"owner open", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; func openSingleModeStorage() { owner, _ := s.New("db"); owner.Close() }`, false},
		{"owner reference escape", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; var open = s.New`, true},
		{"owner argument escape", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; func openSingleModeStorage() { owner, _ := s.New("db"); NewService(owner) }`, true},
		{"owner alias escape", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; func openSingleModeStorage() { owner, _ := s.New("db"); raw := owner; raw.ListNotes() }`, true},
		{"renamed owner escape", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; func openSingleModeStorage() { raw, _ := s.New("db"); NewService(raw) }`, true},
		{"owner workload query", "internal/apiserver/storage.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; func openSingleModeStorage() { owner, _ := s.New("db"); owner.ListNotes() }`, true},
		{"same package authority escape", "internal/auth/extra.go", `package auth; var grant = AuthenticateLocalDatabaseOwner`, true},
		{"handler authority alias", "internal/api/extra.go", `package api; import a "github.com/huynle/brain-api/internal/auth"; var grant = a.AuthenticateLocalDatabaseOwner`, true},
		{"handler offline bypass", "internal/api/extra.go", `package api; import "github.com/huynle/brain-api/internal/tokens"; var grant = tokens.CreateTokenDirect`, true},
		{"no directory exemption", "internal/apiserver/new.go", `package apiserver; import s "github.com/huynle/brain-api/internal/storage"; var open = s.New`, true},
		{"tenant", "internal/service/new.go", `package service; import s "github.com/huynle/brain-api/internal/storage"; type X struct { store *s.TenantStore }`, false},
		{"tests", "internal/api/new_test.go", `package api; import s "github.com/huynle/brain-api/internal/storage"; var open = s.New`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := storageOwnershipPolicy(tc.path, tc.source)
			if err != nil || (len(v) > 0) != tc.denied {
				t.Fatalf("violations=%v err=%v want denied=%v", v, err, tc.denied)
			}
		})
	}
}

func TestProductionStorageOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join(root, ".claude", "worktrees") {
				return filepath.SkipDir
			}
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		v, err := storageOwnershipPolicy(filepath.ToSlash(rel), source)
		if err != nil {
			return err
		}
		for _, msg := range v {
			t.Error(msg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
