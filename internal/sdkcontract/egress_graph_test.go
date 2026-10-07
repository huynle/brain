package sdkcontract

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
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// egressGraph is a conservative, standard-library-only call graph over
// internal/api and internal/service (review nwwa27yh). It is CHA-like:
//   - static calls resolve to their *types.Func;
//   - interface method calls resolve to every analyzed concrete method with the
//     same name whose receiver has all of the interface's method names;
//   - calls through function values (variables, fields, parameters, results)
//     resolve to every address-taken analyzed function with the same signature.
//
// Function literals are attributed to their enclosing declaration, so
// closures, goroutines and defers are included. Nodes are keyed by
// types.Func.FullName so packages loaded from source and from export data
// agree. It over-approximates by design: unexpected reachability must be
// reviewed, never silently ignored.
type egressGraph struct {
	edges    map[string]map[string]bool
	sigs     map[string]string // function key -> signature (without receiver)
	callers  map[string]map[string]bool
	declared map[string]bool // functions declared in analyzed packages
}

const modulePath = "github.com/huynle/brain-api"

var egressPackages = []string{modulePath + "/internal/service", modulePath + "/internal/api"}

func qualifier(p *types.Package) string { return p.Path() }

func funcKey(f *types.Func) string { return f.FullName() }

func buildEgressGraph(t *testing.T) *egressGraph {
	t.Helper()
	cmd := exec.Command("go", "list", "-export", "-deps", "-json", "./internal/api", "./internal/service")
	cmd.Dir = "../.."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export: %v %s", err, stderr.String())
	}
	type listed struct {
		ImportPath, Export, Dir string
		GoFiles                 []string
	}
	exports := map[string]string{}
	sources := map[string]listed{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		sources[p.ImportPath] = p
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file := exports[path]
		if file == "" {
			return nil, os.ErrNotExist
		}
		return os.Open(file)
	})
	g := &egressGraph{edges: map[string]map[string]bool{}, sigs: map[string]string{}, callers: map[string]map[string]bool{}, declared: map[string]bool{}}
	type checked struct {
		files []*ast.File
		info  *types.Info
	}
	var all []checked
	for _, path := range egressPackages {
		src := sources[path]
		var files []*ast.File
		for _, name := range src.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(src.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(path, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", path, err)
		}
		all = append(all, checked{files, info})
	}
	// Pass 1: declarations, concrete methods by name, address-taken values.
	methodsByName := map[string][]*types.Func{}
	methodNames := map[string]map[string]bool{}  // receiver named type -> method names
	addressTaken := map[string]map[string]bool{} // signature -> function keys
	for _, c := range all {
		for _, f := range c.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				fn := c.info.Defs[fd.Name].(*types.Func)
				g.declared[funcKey(fn)] = true
				sig := fn.Type().(*types.Signature)
				g.sigs[funcKey(fn)] = types.TypeString(types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic()), qualifier)
				if recv := sig.Recv(); recv != nil {
					methodsByName[fn.Name()] = append(methodsByName[fn.Name()], fn)
					named := recvName(recv.Type())
					if methodNames[named] == nil {
						methodNames[named] = map[string]bool{}
					}
					methodNames[named][fn.Name()] = true
				}
			}
			callFuns := map[ast.Expr]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					callFuns[ast.Unparen(call.Fun)] = true
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				expr, ok := n.(ast.Expr)
				if !ok || callFuns[expr] {
					return true
				}
				var obj types.Object
				switch e := expr.(type) {
				case *ast.Ident:
					obj = c.info.Uses[e]
				case *ast.SelectorExpr:
					if sel := c.info.Selections[e]; sel != nil {
						obj = sel.Obj()
					} else {
						obj = c.info.Uses[e.Sel]
					}
				}
				fn, ok := obj.(*types.Func)
				if !ok {
					return true
				}
				tv, ok := c.info.Types[expr]
				if !ok {
					return true
				}
				sig := types.TypeString(tv.Type, qualifier)
				if addressTaken[sig] == nil {
					addressTaken[sig] = map[string]bool{}
				}
				addressTaken[sig][funcKey(fn)] = true
				if _, isSel := expr.(*ast.SelectorExpr); isSel {
					return false // do not double-count the selector's Sel ident
				}
				return true
			})
		}
	}
	// Pass 2: call edges.
	for _, c := range all {
		for _, f := range c.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				from := funcKey(c.info.Defs[fd.Name].(*types.Func))
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					fun := ast.Unparen(call.Fun)
					if tv, ok := c.info.Types[fun]; ok && (tv.IsType() || tv.IsBuiltin()) {
						return true
					}
					if _, lit := fun.(*ast.FuncLit); lit {
						return true // body is walked as part of this declaration
					}
					var obj types.Object
					switch e := fun.(type) {
					case *ast.Ident:
						obj = c.info.Uses[e]
					case *ast.SelectorExpr:
						if sel := c.info.Selections[e]; sel != nil {
							obj = sel.Obj()
						} else {
							obj = c.info.Uses[e.Sel]
						}
					case *ast.IndexExpr, *ast.IndexListExpr:
						// generic instantiation: resolve the base identifier
						var base ast.Expr
						if ix, ok := e.(*ast.IndexExpr); ok {
							base = ix.X
						} else {
							base = e.(*ast.IndexListExpr).X
						}
						if id, ok := ast.Unparen(base).(*ast.Ident); ok {
							obj = c.info.Uses[id]
						}
					}
					if fn, ok := obj.(*types.Func); ok {
						sig := fn.Type().(*types.Signature)
						if fn.Origin() != nil {
							fn = fn.Origin()
						}
						g.edge(from, funcKey(fn))
						if g.sigs[funcKey(fn)] == "" {
							g.sigs[funcKey(fn)] = types.TypeString(types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic()), qualifier)
						}
						if recv := sig.Recv(); recv != nil && types.IsInterface(recv.Type()) {
							iface := recv.Type().Underlying().(*types.Interface)
							for _, m := range methodsByName[fn.Name()] {
								have := methodNames[recvName(m.Type().(*types.Signature).Recv().Type())]
								implements := true
								for i := 0; i < iface.NumMethods(); i++ {
									if !have[iface.Method(i).Name()] {
										implements = false
										break
									}
								}
								if implements {
									g.edge(from, funcKey(m))
								}
							}
						}
						return true
					}
					// Dynamic call through a function value: fail closed.
					if tv, ok := c.info.Types[fun]; ok {
						for target := range addressTaken[types.TypeString(tv.Type, qualifier)] {
							g.edge(from, target)
						}
					}
					return true
				})
			}
		}
	}
	return g
}

func recvName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Pkg().Path() + "." + n.Obj().Name()
	}
	return types.TypeString(t, qualifier)
}

func (g *egressGraph) edge(from, to string) {
	if g.edges[from] == nil {
		g.edges[from] = map[string]bool{}
	}
	g.edges[from][to] = true
	if g.callers[to] == nil {
		g.callers[to] = map[string]bool{}
	}
	g.callers[to][from] = true
}

// sinkToken classifies provider sinks. Background refresh is terminal: what
// it calls runs asynchronously and is reported as embedding_background.
func (g *egressGraph) sinkToken(key string) string {
	name := key[strings.LastIndex(key, ".")+1:]
	switch {
	case name == "scheduleEmbeddingRefresh":
		return "embedding_background"
	case name == "indexEmbeddingsForEntry", strings.HasPrefix(name, "IndexEmbeddings"):
		return "embedding_sync"
	case name == "Embed" && strings.Contains(key, "/internal/service"):
		return "embedding_sync"
	case name == "Enqueue" && strings.Contains(g.sigs[key], "phonepush.PushMessage"):
		return "web_push"
	}
	return ""
}

// reach returns the provider tokens reachable from root and one witness path
// per token.
func (g *egressGraph) reach(root string) map[string][]string {
	found := map[string][]string{}
	parent := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if tok := g.sinkToken(n); tok != "" {
			if _, seen := found[tok]; !seen {
				var path []string
				for p := n; p != ""; p = parent[p] {
					path = append([]string{p}, path...)
				}
				found[tok] = path
			}
			if tok == "embedding_background" {
				continue
			}
		}
		for next := range g.edges[n] {
			if _, seen := parent[next]; !seen {
				parent[next] = n
				queue = append(queue, next)
			}
		}
	}
	return found
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
