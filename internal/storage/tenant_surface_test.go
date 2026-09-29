package storage_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Load the actual production package's compiler export data, not a synthetic
// replacement of TenantStore. AST inventories separately scan inactive build tags.
func TestTenantProductionTypeSurface(t *testing.T) {
	const path = "github.com/huynle/brain-api/internal/storage"
	out, err := exec.Command("go", "list", "-deps", "-export", "-json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct{ ImportPath, Export string }
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })
	pkg, err := imp.Import(path)
	if err != nil {
		t.Fatal(err)
	}
	view := pkg.Scope().Lookup("TenantStore").Type()
	for _, typ := range []types.Type{view, types.NewPointer(view)} {
		for _, name := range []string{"DB", "ValidateToken", "StorageLayer", "Close", "ForTenant", "Control", "SingleModeTokens", "Owner", "Backing"} {
			if obj, _, _ := types.LookupFieldOrMethod(typ, true, nil, name); obj != nil {
				t.Errorf("%s exposes %s", typ, name)
			}
		}
	}
	// No embedded owner, hidden owner field, or exported backing field either.
	fields := view.Underlying().(*types.Struct)
	if fields.NumFields() != 2 {
		t.Errorf("TenantStore has %d fields, want only pool and tenant binding", fields.NumFields())
	}
	sqlPackage, err := imp.Import("database/sql")
	if err != nil {
		t.Fatal(err)
	}
	tenantPackage, err := imp.Import("github.com/huynle/brain-api/internal/tenant")
	if err != nil {
		t.Fatal(err)
	}
	wantFields := map[string]types.Type{"db": types.NewPointer(sqlPackage.Scope().Lookup("DB").Type()), "tenantID": tenantPackage.Scope().Lookup("ID").Type()}
	for i := 0; i < fields.NumFields(); i++ {
		f := fields.Field(i)
		if want, ok := wantFields[f.Name()]; !ok || !types.Identical(f.Type(), want) {
			t.Errorf("unexpected tenant field: %s", f)
		}
		if f.Embedded() || f.Exported() || strings.Contains(f.Type().String(), "StorageLayer") {
			t.Errorf("tenant backing escape: %s", f)
		}
	}
	methods := types.NewMethodSet(types.NewPointer(view))
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		if !method.Exported() {
			continue
		}
		results := method.Type().(*types.Signature).Results().String()
		for _, forbidden := range []string{"storage.StorageLayer", "storage.ControlStore", "storage.TokenAdmin", "storage.SingleModeTokenStore", "sql.DB", "tenantfs.Repository"} {
			if strings.Contains(results, forbidden) {
				t.Errorf("owner/control return escape: %s", method)
			}
		}
	}
	check := func(body string) error {
		f, err := parser.ParseFile(fset, "consumer.go", "package consumer\nimport s \""+path+"\"\n"+body, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&types.Config{Importer: imp}).Check("github.com/huynle/brain-api/consumer", fset, []*ast.File{f}, nil)
		return err
	}
	for _, receiver := range []string{"s.TenantStore", "*s.TenantStore"} {
		for _, name := range []string{"DB", "ValidateToken", "StorageLayer", "Close", "ForTenant", "Control", "SingleModeTokens", "Owner", "Backing"} {
			if err := check("func f(v " + receiver + ") { _ = v." + name + " }"); err == nil {
				t.Errorf("external selector %s.%s accepted", receiver, name)
			}
		}
		for _, contract := range []string{"DB() *sql.DB", "ValidateToken(context.Context, string) (*s.Token, error)", "Close() error"} {
			if err := check("import (\"context\"; \"database/sql\")\nvar _ context.Context; var _ *sql.DB; var v " + receiver + "; var _ interface { " + contract + " } = v"); err == nil {
				t.Errorf("external interface %s implements %s", receiver, contract)
			}
		}
	}
	if err := check("func f(v *s.TenantStore) { _ = v.ListNotes }; var _ interface { Close() error } = (*s.StorageLayer)(nil)"); err != nil {
		t.Fatalf("positive scoped/owner control: %v", err)
	}
	if err := check(`import "context"
var _ interface { ValidateToken(context.Context, string) (*s.Token, error) } = (*s.ControlStore)(nil)
var _ interface { ListNotes(context.Context, *s.ListOptions) ([]*s.NoteRow, error) } = (*s.TenantStore)(nil)`); err != nil {
		t.Fatalf("positive interface controls: %v", err)
	}
}
