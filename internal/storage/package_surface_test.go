package storage

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// Separate from receiver definitions and selector occurrences: moving a raw
// workload method to an exported package function must not erase the debt.
func collectStorageFunctions(tree fs.FS) (debtSet, error) {
	entries, err := fs.ReadDir(tree, "internal/storage")
	if err != nil {
		return nil, err
	}
	functions := debtSet{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := fs.ReadFile(tree, "internal/storage/"+name)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, data, 0) // Includes inactive build tags.
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			functions[fn.Name.Name] = true
			// These two exemptions are pure constructors of values, NOT DB
			// constructors. Pin their complete declarations, including signatures,
			// rather than permitting calls to SQL or hidden helper wrappers.
			if expected, pure := pureStorageFunctions[fn.Name.Name]; pure {
				var actual bytes.Buffer
				if err := format.Node(&actual, fset, fn); err != nil {
					return nil, err
				}
				want, err := format.Source([]byte(expected))
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(actual.String()) != strings.TrimSpace(string(want)) {
					return nil, fmt.Errorf("pure package helper %s changed; SQL/delegation laundering forbidden without review", fn.Name.Name)
				}
			}
		}
	}
	return functions, nil
}

var pureStorageFunctions = map[string]string{
	"ProjectPathPrefix": `func ProjectPathPrefix(projectID string) string {
	return "projects/" + projectID + "/"
}`,
	"DefaultProjectPlacement": `func DefaultProjectPlacement(projectID string) *ProjectPlacementRow {
	return &ProjectPlacementRow{
		ProjectID: projectID,
		Affinity: "soft",
		PreferredMachines: []string{},
		AllowedMachines: []string{},
		WorkspacePolicy: "",
		RequiredLabels: map[string]string{},
		RequiredCapabilities: []string{},
		ResourceRequirements: map[string]any{},
	}
}`,
}

func TestFinalStorageDefinitionAllowlists(t *testing.T) {
	methods, _, err := collectUnscoped(os.DirFS("../.."), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := formatDebt(methods); got != "Close\nControl\nForTenant\nSingleModeTokens\n" {
		t.Fatalf("final raw methods (private included):\n%s", got)
	}
	functions, err := collectStorageFunctions(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/storage_package_functions.golden")
	if err != nil {
		t.Fatal(err)
	}
	if formatDebt(parseDebt(string(data))) != string(data) {
		t.Fatal("package function golden not canonical")
	}
	if got := formatDebt(functions); got != string(data) {
		t.Fatalf("exported package functions differ: actual %d, allowed %d\n%s", len(functions), len(parseDebt(string(data))), got)
	}
}

func TestStoragePackageFunctionMutations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		denied       bool
	}{
		{"pure", pureStorageFunctions["ProjectPathPrefix"], false},
		{"SQL laundering", `func ProjectPathPrefix(projectID string) string { db.Query("SELECT * FROM notes"); return projectID }`, true},
		{"helper laundering", `func ProjectPathPrefix(projectID string) string { return hiddenQuery(projectID) }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := fstest.MapFS{"internal/storage/hidden.go": {Data: []byte("//go:build never_enabled\n\npackage storage\n" + tc.source)}}
			_, err := collectStorageFunctions(tree)
			if (err != nil) != tc.denied {
				t.Fatalf("denied=%v err=%v", tc.denied, err)
			}
		})
	}
	tree := fstest.MapFS{"internal/storage/hidden.go": {Data: []byte("//go:build never_enabled\n\npackage storage\nfunc DumpNotes(db *sql.DB) { db.Query(`SELECT * FROM notes`) }\nfunc (s StorageLayer) hiddenQuery() {}")}}
	functions, err := collectStorageFunctions(tree)
	if err != nil || !functions["DumpNotes"] {
		t.Fatalf("missed build-tagged package function: %v %v", functions, err)
	}
	data, err := os.ReadFile("testdata/storage_package_functions.golden")
	if err != nil {
		t.Fatal(err)
	}
	added, _ := compareDebt(functions, parseDebt(string(data)))
	if !added["DumpNotes"] {
		t.Fatal("workload function laundering admitted")
	}
	methods, _, err := collectUnscoped(tree, nil)
	if err != nil || !methods["hiddenQuery"] {
		t.Fatalf("missed private value receiver: %v %v", methods, err)
	}
}

func TestProductionStoragePackageFunctionsBaseline(t *testing.T) {
	ref, requested := os.LookupEnv("BRAIN_STORAGE_RATCHET_BASE")
	if !requested {
		if os.Getenv("GITHUB_EVENT_NAME") == "pull_request" {
			t.Fatal("BRAIN_STORAGE_RATCHET_BASE required")
		}
		t.Skip("historical package-function check requires trusted base; exact current check remains active")
	}
	base, err := unscopedGitTree("../..", ref)
	if err != nil {
		t.Fatal(err)
	}
	// Independent exported-name monotonic check, with actual-base bootstrapping.
	// The two pure helpers also retain their reviewed no-I/O definitions.
	baseFunctions, err := collectStorageFunctions(base)
	if err != nil {
		t.Fatal(err)
	}
	headFunctions, err := collectStorageFunctions(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	baseGolden := baseFunctions
	const golden = "internal/storage/testdata/storage_package_functions.golden"
	if data, err := fs.ReadFile(base, golden); err == nil {
		baseGolden = parseDebt(string(data))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	headGolden, err := os.ReadFile("../../" + golden)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]debtSet{{headFunctions, baseFunctions}, {parseDebt(string(headGolden)), baseGolden}} {
		added, _ := compareDebt(pair[0], pair[1])
		if len(added) != 0 {
			t.Errorf("package-function growth against trusted base:\n%s", formatDebt(added))
		}
	}
}
