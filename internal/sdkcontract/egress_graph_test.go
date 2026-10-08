package sdkcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"sync"
	"testing"
)

// egressGraph is a conservative, standard-library-only call graph over EVERY
// non-test package in the module (reviews nwwa27yh, pcteuxwj). It is a
// policy-accuracy check, not a full verifier. Resolution:
//   - static calls resolve to their *types.Func;
//   - interface method calls resolve to every module method of that name
//     whose receiver has all of the interface's method names;
//   - calls through function values resolve to every address-taken module
//     function or function literal with an identical signature, or, when the
//     called value's type mentions a type parameter, with the same arity;
//   - every function literal (including package-level var initializers) is its
//     own address-taken node, with an edge from the code that creates it.
//
// Known limits (documented, not hidden):
//   - reflection (reflect.Value.Call/Method/MethodByName) is not modeled;
//     TestNoUnreviewedReflectiveCalls forbids it outside an allowlist;
//   - a call through a function value resolves only to targets declared in the
//     calling package or the packages it imports. A callback created in a
//     higher-level package and invoked by a lower-level one is attributed to
//     its creator (every literal has a creation edge), not its invoker; event
//     subscriptions are covered by the policy's event_fanout declaration;
//   - niladic func() values resolve within the calling package only;
//   - a call through a parameter of a function that is never address-taken
//     adds no edges ONLY when every static caller passed a resolved value
//     (a function, method value or literal, which the caller then carries);
//     a variable, field or pass-through argument makes it signature-matched;
//   - interface method values expand to every module implementation.
type egressGraph struct {
	edges     map[string]map[string]bool
	direct    map[string]map[string]bool // static calls to a concrete function or method (no interface dispatch)
	sigs      map[string]string          // node -> signature (without receiver)
	callers   map[string]map[string]bool
	declared  map[string]bool            // module FuncDecls and literals
	enclosing map[string]string          // literal node -> enclosing declaration (stable review name)
	goRoots   map[string]bool            // targets of go statements
	entries   []string                   // main.main, init, package var initializers
	address   map[string]bool            // address-taken module nodes
	reflect   []string                   // reflective call sites "file:line in node"
	routerRef map[string]bool            // functions referenced from internal/api/router.go
	tokens    map[string]map[string]bool // node -> provider tokens reachable (memoized)
}

var (
	egressOnce  sync.Once
	egressCache *egressGraph
	egressErr   string
)

// sharedEgressGraph builds the whole-module graph once per test process.
func sharedEgressGraph(t *testing.T) *egressGraph {
	t.Helper()
	egressOnce.Do(func() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					egressErr = fmt.Sprint(r)
				}
			}()
			egressCache = buildEgressGraphOrPanic()
		}()
	})
	if egressCache == nil {
		t.Fatalf("egress graph: %s", egressErr)
	}
	return egressCache
}

// panicT adapts buildEgressGraph's fatal reporting for the shared builder.
type panicT struct{ testing.TB }

func (panicT) Helper()                           {}
func (panicT) Fatal(args ...any)                 { panic(fmt.Sprint(args...)) }
func (panicT) Fatalf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }

func buildEgressGraphOrPanic() *egressGraph { return buildEgressGraph(panicT{}) }

const modulePath = "github.com/huynle/brain-api"

func qualifier(p *types.Package) string { return p.Path() }

func funcKey(f *types.Func) string { return f.FullName() }

// sigString renders a signature WITHOUT receiver or parameter names, so a
// method, a named function and an unnamed func type compare equal.
func sigString(sig *types.Signature) string {
	strip := func(tuple *types.Tuple) *types.Tuple {
		vars := make([]*types.Var, tuple.Len())
		for i := range vars {
			vars[i] = types.NewVar(token.NoPos, nil, "", tuple.At(i).Type())
		}
		return types.NewTuple(vars...)
	}
	return types.TypeString(types.NewSignatureType(nil, nil, nil, strip(sig.Params()), strip(sig.Results()), sig.Variadic()), qualifier)
}

// typeSig normalizes any function-valued type for signature matching.
func typeSig(t types.Type) string {
	if sig, ok := t.Underlying().(*types.Signature); ok {
		return sigString(sig)
	}
	return types.TypeString(t, qualifier)
}

func hasTypeParam(t types.Type) bool {
	found := false
	var visit func(types.Type)
	visit = func(t types.Type) {
		if found || t == nil {
			return
		}
		switch u := t.(type) {
		case *types.TypeParam:
			found = true
		case *types.Signature:
			for i := 0; i < u.Params().Len(); i++ {
				visit(u.Params().At(i).Type())
			}
			for i := 0; i < u.Results().Len(); i++ {
				visit(u.Results().At(i).Type())
			}
		case *types.Slice:
			visit(u.Elem())
		case *types.Pointer:
			visit(u.Elem())
		case *types.Map:
			visit(u.Key())
			visit(u.Elem())
		case *types.Chan:
			visit(u.Elem())
		case *types.Array:
			visit(u.Elem())
		}
	}
	visit(t)
	return found
}

func arity(t types.Type) string {
	if sig, ok := t.Underlying().(*types.Signature); ok {
		return fmt.Sprintf("%d/%d", sig.Params().Len(), sig.Results().Len())
	}
	return ""
}

func buildEgressGraph(t testing.TB) *egressGraph {
	t.Helper()
	cmd := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	cmd.Dir = "../.."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export: %v %s", err, stderr.String())
	}
	type listed struct {
		ImportPath, Export, Dir, Name string
		GoFiles, CgoFiles, Deps       []string
		Module                        *struct{ Path string }
	}
	exports := map[string]string{}
	var modulePkgs []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if p.Module != nil && p.Module.Path == modulePath && len(p.GoFiles) > 0 {
			if len(p.CgoFiles) > 0 {
				t.Fatalf("%s uses cgo: not analyzable here", p.ImportPath)
			}
			modulePkgs = append(modulePkgs, p)
		}
	}
	sort.Slice(modulePkgs, func(i, j int) bool { return modulePkgs[i].ImportPath < modulePkgs[j].ImportPath })
	// visible[p] = p plus every package it imports (transitively): the only
	// places a function value called in p can have been created without
	// first passing through p's own API (creation edges cover the rest).
	visible := map[string]map[string]bool{}
	for _, p := range modulePkgs {
		v := map[string]bool{p.ImportPath: true}
		for _, dep := range p.Deps {
			v[dep] = true
		}
		visible[p.ImportPath] = v
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file := exports[path]
		if file == "" {
			return nil, os.ErrNotExist
		}
		return os.Open(file)
	})
	g := &egressGraph{edges: map[string]map[string]bool{}, direct: map[string]map[string]bool{}, sigs: map[string]string{}, callers: map[string]map[string]bool{}, declared: map[string]bool{}, enclosing: map[string]string{}, goRoots: map[string]bool{}, address: map[string]bool{}, routerRef: map[string]bool{}}
	type checked struct {
		path, name string
		files      []*ast.File
		info       *types.Info
	}
	var all []checked
	for _, p := range modulePkgs {
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		all = append(all, checked{p.ImportPath, p.Name, files, info})
	}
	litKey := func(pkg string, lit *ast.FuncLit) string {
		pos := fset.Position(lit.Pos())
		return fmt.Sprintf("%s.$lit@%s:%d:%d", pkg, filepath.Base(pos.Filename), pos.Line, pos.Column)
	}
	resolve := func(info *types.Info, fun ast.Expr) types.Object {
		switch e := fun.(type) {
		case *ast.Ident:
			return info.Uses[e]
		case *ast.SelectorExpr:
			if sel := info.Selections[e]; sel != nil {
				return sel.Obj()
			}
			return info.Uses[e.Sel]
		case *ast.IndexExpr:
			return resolveBase(info, e.X)
		case *ast.IndexListExpr:
			return resolveBase(info, e.X)
		}
		return nil
	}
	// Pass 1: declarations, method sets by name, address-taken values.
	methodsByName := map[string][]string{}
	methodNames := map[string]map[string]bool{}
	bySig := map[string]map[string]bool{}
	byArity := map[string]map[string]bool{}
	addressTaken := func(key string, typ types.Type) {
		g.address[key] = true
		for _, idx := range []struct {
			m map[string]map[string]bool
			k string
		}{{bySig, typeSig(typ)}, {byArity, arity(typ)}} {
			if idx.m[idx.k] == nil {
				idx.m[idx.k] = map[string]bool{}
			}
			idx.m[idx.k][key] = true
		}
	}
	paramOwner := map[*types.Var]string{} // FuncDecl parameter -> owning function
	paramIndex := map[*types.Var]int{}
	unresolvedParam := map[string]bool{} // "owner#i": some caller passed an unresolved function value
	type pendingCall struct {
		from, owner, pkg string
		idx              int
		typ              types.Type
	}
	var pending []pendingCall
	type ifaceValue struct {
		fn  *types.Func
		typ types.Type
	}
	var ifaceValues []ifaceValue
	closureLit := map[*ast.FuncLit]bool{}
	closureTargets := map[*types.Var][]string{}
	for _, c := range all {
		for _, f := range c.files {
			isRouter := c.path == modulePath+"/internal/api" && filepath.Base(fset.Position(f.Pos()).Filename) == "router.go"
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				fn := c.info.Defs[fd.Name].(*types.Func)
				key := funcKey(fn)
				g.declared[key] = true
				idx := 0
				for _, field := range fd.Type.Params.List {
					if len(field.Names) == 0 {
						idx++
						continue
					}
					for _, name := range field.Names {
						if v, ok := c.info.Defs[name].(*types.Var); ok {
							paramOwner[v] = key
							paramIndex[v] = idx
						}
						idx++
					}
				}
				sig := fn.Type().(*types.Signature)
				g.sigs[key] = sigString(sig)
				if recv := sig.Recv(); recv != nil {
					methodsByName[fn.Name()] = append(methodsByName[fn.Name()], key)
					named := recvName(recv.Type())
					if methodNames[named] == nil {
						methodNames[named] = map[string]bool{}
					}
					methodNames[named][fn.Name()] = true
				}
				if (c.name == "main" && fd.Name.Name == "main" && fd.Recv == nil) || (fd.Name.Name == "init" && fd.Recv == nil) {
					g.entries = append(g.entries, key)
				}
			}
			callFuns := map[ast.Expr]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					callFuns[ast.Unparen(call.Fun)] = true
				}
				return true
			})
			// Local closure variables: a function literal assigned to a
			// function-local variable that is ONLY ever called cannot escape;
			// calls through it resolve exactly to its literals.
			localLits := map[*types.Var][]*ast.FuncLit{}
			impure := map[*types.Var]bool{}
			record := func(lhs ast.Expr, rhs ast.Expr) {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					return
				}
				obj := c.info.Defs[id]
				if obj == nil {
					obj = c.info.Uses[id]
				}
				v, ok := obj.(*types.Var)
				if !ok || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
					return
				}
				if lit, ok := ast.Unparen(rhs).(*ast.FuncLit); ok {
					localLits[v] = append(localLits[v], lit)
				} else {
					impure[v] = true
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					if len(x.Lhs) == len(x.Rhs) {
						for i := range x.Lhs {
							record(x.Lhs[i], x.Rhs[i])
						}
					}
				case *ast.ValueSpec:
					if len(x.Names) == len(x.Values) {
						for i := range x.Names {
							record(x.Names[i], x.Values[i])
						}
					}
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || callFuns[id] {
					return true
				}
				if v, ok := c.info.Uses[id].(*types.Var); ok && localLits[v] != nil {
					impure[v] = true // escapes: used as a value, not only called
				}
				return true
			})
			for v, lits := range localLits {
				if impure[v] {
					continue
				}
				for _, lit := range lits {
					closureLit[lit] = true
				}
				for _, lit := range lits {
					closureTargets[v] = append(closureTargets[v], litKey(c.path, lit))
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					key := litKey(c.path, lit)
					g.declared[key] = true
					if tv, ok := c.info.Types[lit]; ok {
						g.sigs[key] = typeSig(tv.Type)
						// A literal invoked in place (func(){}(), go/defer
						// func(){}()) is never stored, so no other call can
						// reach it. Only literals used as values are targets.
						if !callFuns[lit] && !closureLit[lit] {
							addressTaken(key, tv.Type)
						}
					}
					return true
				}
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
				if fn.Origin() != nil {
					fn = fn.Origin()
				}
				if tv, ok := c.info.Types[expr]; ok {
					addressTaken(funcKey(fn), tv.Type)
					if recv := fn.Type().(*types.Signature).Recv(); recv != nil && types.IsInterface(recv.Type()) {
						ifaceValues = append(ifaceValues, ifaceValue{fn, tv.Type})
					}
				}
				if isRouter {
					g.routerRef[funcKey(fn)] = true
				}
				if _, isSel := expr.(*ast.SelectorExpr); isSel {
					return false
				}
				return true
			})
		}
	}
	// implementations returns every module method implementing an interface
	// method (by method set names), as used for direct interface calls.
	implementations := func(fn *types.Func) []string {
		recv := fn.Type().(*types.Signature).Recv()
		if recv == nil || !types.IsInterface(recv.Type()) {
			return nil
		}
		iface := recv.Type().Underlying().(*types.Interface)
		var out []string
		for _, m := range methodsByName[fn.Name()] {
			have := methodNames[recvOf(m)]
			implements := true
			for i := 0; i < iface.NumMethods(); i++ {
				if !have[iface.Method(i).Name()] {
					implements = false
					break
				}
			}
			if implements {
				out = append(out, m)
			}
		}
		return out
	}
	// An interface method VALUE (h.brain.UpdateMetadata) may be any module
	// implementation: register each as address-taken with the value's type.
	for _, v := range ifaceValues {
		for _, m := range implementations(v.fn) {
			addressTaken(m, v.typ)
			// The interface method node itself dispatches to every
			// implementation, so a path reaching the value (argument, local
			// variable, signature-matched dynamic call) reaches them all.
			g.edge(funcKey(v.fn), m)
		}
	}
	// addSigEdges: a call through a function value of type typ, made in pkg,
	// may reach any visible address-taken target with the same signature.
	addSigEdges := func(from, pkg string, typ types.Type) {
		sig := typeSig(typ)
		targets := bySig[sig]
		if hasTypeParam(typ) {
			targets = byArity[arity(typ)]
		}
		for target := range targets {
			// Niladic func() values (cancel, once.Do, launch hooks) are
			// pervasive; resolve them within the calling package. Each literal
			// still has a creation edge from the code that creates it.
			if sig == "func()" && !inPackage(target, pkg) {
				continue
			}
			if !visible[pkg][nodePkg(target)] {
				continue
			}
			g.edge(from, target)
		}
	}
	// Pass 2: edges. Each FuncDecl body and each literal is its own node.
	for _, c := range all {
		var walk func(from string, body ast.Node)
		walk = func(from string, body ast.Node) {
			ast.Inspect(body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncLit:
					key := litKey(c.path, x)
					g.edge(from, key)
					if encl, ok := g.enclosing[from]; ok {
						g.enclosing[key] = encl
					} else {
						g.enclosing[key] = from
					}
					walk(key, x.Body)
					return false
				case *ast.GoStmt:
					fun := ast.Unparen(x.Call.Fun)
					if lit, ok := fun.(*ast.FuncLit); ok {
						g.goRoots[litKey(c.path, lit)] = true
					} else if fn, ok := resolve(c.info, fun).(*types.Func); ok {
						if fn.Origin() != nil {
							fn = fn.Origin()
						}
						g.goRoots[funcKey(fn)] = true
					}
					return true
				case *ast.CallExpr:
					fun := ast.Unparen(x.Fun)
					if tv, ok := c.info.Types[fun]; ok && (tv.IsType() || tv.IsBuiltin()) {
						return true
					}
					if _, lit := fun.(*ast.FuncLit); lit {
						return true // the literal node is created and walked; edge exists
					}
					if fn, ok := resolve(c.info, fun).(*types.Func); ok {
						if fn.Origin() != nil {
							fn = fn.Origin()
						}
						if fn.Pkg() != nil && fn.Pkg().Path() == "reflect" {
							switch fn.Name() {
							case "Call", "CallSlice", "Method", "MethodByName":
								pos := fset.Position(x.Pos())
								g.reflect = append(g.reflect, fmt.Sprintf("%s:%d %s.%s", filepath.Base(pos.Filename), pos.Line, c.path, fn.Name()))
							}
						}
						key := funcKey(fn)
						g.edge(from, key)
						// The caller chooses what a higher-order callee runs:
						// attribute function-valued arguments to the caller.
						sig := fn.Type().(*types.Signature)
						for i, arg := range x.Args {
							a := ast.Unparen(arg)
							tv, ok := c.info.Types[a]
							if !ok {
								continue
							}
							if _, isFunc := tv.Type.Underlying().(*types.Signature); !isFunc {
								continue
							}
							resolved := false
							switch av := a.(type) {
							case *ast.FuncLit:
								resolved = true // creation edge is added when the literal is walked
							case *ast.Ident, *ast.SelectorExpr:
								if af, ok := resolve(c.info, av).(*types.Func); ok {
									if af.Origin() != nil {
										af = af.Origin()
									}
									g.edge(from, funcKey(af))
									for _, m := range implementations(af) {
										g.edge(from, m)
									}
									resolved = true
								} else if id, ok := av.(*ast.Ident); ok {
									if v, ok := c.info.Uses[id].(*types.Var); ok && closureTargets[v] != nil {
										for _, target := range closureTargets[v] {
											g.edge(from, target)
										}
										resolved = true
									}
								}
							}
							if !resolved {
								// A variable, field or pass-through parameter: the
								// callee's calls through this parameter must stay
								// signature-matched (over-approximation).
								pi := i
								if pi >= sig.Params().Len() {
									pi = sig.Params().Len() - 1
								}
								unresolvedParam[fmt.Sprintf("%s#%d", key, pi)] = true
							}
						}
						if g.sigs[key] == "" {
							g.sigs[key] = sigString(sig)
						}
						impls := implementations(fn)
						for _, m := range impls {
							g.edge(from, m) // direct interface call: every implementation
						}
						if len(impls) == 0 {
							if g.direct[from] == nil {
								g.direct[from] = map[string]bool{}
							}
							g.direct[from][key] = true
						}
						return true
					}
					// Call through a non-escaping local closure: exact targets.
					if id, ok := fun.(*ast.Ident); ok {
						if v, ok := c.info.Uses[id].(*types.Var); ok && closureTargets[v] != nil {
							for _, target := range closureTargets[v] {
								g.edge(from, target)
							}
							return true
						}
					}
					// Call through a parameter of a module function whose every
					// call site is static: each caller already carries the
					// effects of the argument it passed (see above).
					if id, ok := fun.(*ast.Ident); ok {
						if v, ok := c.info.Uses[id].(*types.Var); ok {
							if owner, isParam := paramOwner[v]; isParam {
								if tv, ok := c.info.Types[fun]; ok {
									pending = append(pending, pendingCall{from, owner, c.path, paramIndex[v], tv.Type})
								}
								return true
							}
						}
					}
					// Dynamic call through a function value: fail closed.
					if tv, ok := c.info.Types[fun]; ok {
						addSigEdges(from, c.path, tv.Type)
					}
					return true
				}
				return true
			})
		}
		for _, f := range c.files {
			for _, d := range f.Decls {
				switch decl := d.(type) {
				case *ast.FuncDecl:
					if decl.Body != nil {
						walk(funcKey(c.info.Defs[decl.Name].(*types.Func)), decl.Body)
					}
				case *ast.GenDecl:
					// Package-level initializers run at program start.
					initKey := c.path + ".$varinit"
					for _, spec := range decl.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							for _, v := range vs.Values {
								g.declared[initKey] = true
								walk(initKey, v)
							}
						}
					}
				}
			}
		}
		if g.declared[c.path+".$varinit"] {
			g.entries = append(g.entries, c.path+".$varinit")
		}
	}
	// Calls through a helper's function parameter are skipped only when the
	// helper is never address-taken AND every caller passed a resolved value;
	// otherwise they are signature-matched like any dynamic call.
	for _, pc := range pending {
		if g.address[pc.owner] || unresolvedParam[fmt.Sprintf("%s#%d", pc.owner, pc.idx)] {
			addSigEdges(pc.from, pc.pkg, pc.typ)
		}
	}
	sort.Strings(g.entries)
	g.propagateTokens()
	return g
}

// propagateTokens computes, for every node, the provider tokens a forward
// traversal (as in reach, without cuts/stops) would find: tokens flow backward
// from each sink along caller edges, never passing THROUGH a background
// refresh node (forward traversal stops there).
func (g *egressGraph) propagateTokens() {
	g.tokens = map[string]map[string]bool{}
	nodes := map[string]bool{}
	for n := range g.edges {
		nodes[n] = true
		for m := range g.edges[n] {
			nodes[m] = true
		}
	}
	for _, tok := range []string{"embedding_sync", "embedding_background", "web_push"} {
		var queue []string
		seen := map[string]bool{}
		for n := range nodes {
			if g.sinkToken(n) == tok {
				seen[n] = true
				queue = append(queue, n)
			}
		}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			if tok != "embedding_background" && g.sinkToken(n) == "embedding_background" {
				continue // forward traversal never continues past background refresh
			}
			if g.tokens[n] == nil {
				g.tokens[n] = map[string]bool{}
			}
			g.tokens[n][tok] = true
			for caller := range g.callers[n] {
				if !seen[caller] {
					seen[caller] = true
					queue = append(queue, caller)
				}
			}
		}
	}
}

// nodePkg returns the package path of a node key.
func nodePkg(key string) string {
	if i := strings.Index(key, ".$"); i >= 0 {
		return key[:i]
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(key, "("), "*")
	if i := strings.Index(trimmed, ")."); i >= 0 {
		trimmed = trimmed[:i]
	}
	if i := strings.LastIndex(trimmed, "."); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

// inPackage reports whether a node key belongs to pkg: "pkg.F", "pkg.$lit@…",
// "(pkg.T).M" or "(*pkg.T).M".
func inPackage(key, pkg string) bool {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(key, "("), "*")
	return strings.HasPrefix(trimmed, pkg+".") && !strings.Contains(strings.TrimPrefix(trimmed, pkg+"."), "/")
}

func resolveBase(info *types.Info, x ast.Expr) types.Object {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		return info.Uses[e]
	case *ast.SelectorExpr:
		if sel := info.Selections[e]; sel != nil {
			return sel.Obj()
		}
		return info.Uses[e.Sel]
	}
	return nil
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

// recvOf extracts the receiver type name from a method FullName key such as
// "(*pkg.T).M" or "(pkg.T).M".
func recvOf(key string) string {
	inner := strings.TrimPrefix(key, "(")
	inner = strings.TrimPrefix(inner, "*")
	end := strings.Index(inner, ").")
	if end < 0 {
		return ""
	}
	return inner[:end]
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

// sinkToken classifies provider sinks by name AND signature. Background
// refresh is terminal: what it calls runs asynchronously.
func (g *egressGraph) sinkToken(key string) string {
	name := key[strings.LastIndex(key, ".")+1:]
	switch {
	case name == "scheduleEmbeddingRefresh":
		return "embedding_background"
	case name == "indexEmbeddingsForEntry", strings.HasPrefix(name, "IndexEmbeddings"):
		return "embedding_sync"
	case name == "Embed" && g.sigs[key] == "func(context.Context, []string) ([][]float32, error)":
		return "embedding_sync"
	case name == "Enqueue" && strings.Contains(g.sigs[key], "phonepush.PushMessage"):
		return "web_push"
	}
	return ""
}

type cutEdge struct{ from, to string }

// reach returns the provider tokens reachable from root and one witness path
// per token. Traversal does not cross cut edges, and does not enter stop
// nodes (other than the root itself).
func (g *egressGraph) reach(root string, cuts map[cutEdge]bool, stop map[string]bool) map[string][]string {
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
		for _, next := range sortedKeys(g.edges[n]) {
			if cuts[cutEdge{n, next}] || stop[next] {
				continue
			}
			if _, seen := parent[next]; !seen {
				parent[next] = n
				queue = append(queue, next)
			}
		}
	}
	return found
}

// closure returns every node reachable from roots (not crossing stop nodes).
func (g *egressGraph) closure(roots []string, stop map[string]bool) map[string]bool {
	seen := map[string]bool{}
	stack := append([]string(nil), roots...)
	for _, r := range roots {
		seen[r] = true
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.edges[n] {
			if !seen[next] && !stop[next] {
				seen[next] = true
				stack = append(stack, next)
			}
		}
	}
	return seen
}

// reviewName gives a stable, line-independent name for a node: literals are
// reported as their enclosing declaration.
func (g *egressGraph) reviewName(key string) string {
	if encl, ok := g.enclosing[key]; ok {
		return encl
	}
	return key
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
