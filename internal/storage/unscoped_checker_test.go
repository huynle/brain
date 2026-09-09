package storage

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUnscopedChecker(t *testing.T) {
	tree := fstest.MapFS{
		"internal/storage/a.go": {Data: []byte(`package storage
type StorageLayer struct{}
func (*StorageLayer) Foo() {}
func (StorageLayer) value() {}
func (*Other) Nope() {}
func NotAMethod() {}
`)},
		"internal/storage/tagged.go": {Data: []byte("//go:build never_enabled\n\npackage storage\nfunc (*StorageLayer) private() {}")},
		"internal/storage/a_test.go": {Data: []byte("not even valid Go")},
		"internal/service/a.go": {Data: []byte(`package service
import database "database/sql"
func f() { x.Foo(); _ = x.Foo; x.Close(); x.DB() }
func (x *Wrapper) Forward() *database.DB { return x.DB() }
`)},
		"vendor/bad.go":     {Data: []byte("invalid")},
		".worktrees/bad.go": {Data: []byte("invalid")},
	}
	methods, sites, err := collectUnscoped(tree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := formatDebt(methods); got != "Foo\nprivate\nvalue\n" {
		t.Fatalf("definitions = %q, want pointer/value/private, excluding tests and other receivers", got)
	}
	wantSites := "internal/service/a.go:Wrapper.Forward DB#1 DB#2 db-return#1\ninternal/service/a.go:f DB#1 Foo#1 Foo#2\n"
	if got := formatDebt(sites); got != wantSites {
		t.Fatalf("inventory = %q, want %q", got, wantSites)
	}
	if !reflect.DeepEqual(sites, parseDebt(formatDebt(sites))) {
		t.Fatal("inventory round trip changed set")
	}
	// Alternate tree collection must not silently use the checkout's files.
	delete(tree, "internal/storage/a.go")
	methods, _, err = collectUnscoped(tree, nil)
	if err != nil || formatDebt(methods) != "private\n" {
		t.Fatalf("alternate tree methods=%v err=%v", methods, err)
	}
	// A fixed baseline vocabulary still inventories Foo after its receiver moves.
	_, sites, err = collectUnscoped(tree, parseDebt("Foo\n"))
	if err != nil || !strings.Contains(formatDebt(sites), "Foo#2") {
		t.Fatalf("baseline vocabulary lost call debt: %v %v", sites, err)
	}
}

func TestUnscopedInventoryConservativeDB(t *testing.T) {
	tree := fstest.MapFS{
		"internal/service/db.go": {Data: []byte(`package service
import sql "database/sql"
func f(sql any) { _ = sql.DB }
func (w Wrapper) DB() *sql.DB { return w.store.DB() }
var forward = (*Wrapper).DB
`)},
	}
	_, sites, err := collectUnscoped(tree, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "internal/service/db.go:Wrapper.DB DB#1 DB#2 db-return#1\ninternal/service/db.go:f DB#1\ninternal/service/db.go:package DB#1\n"
	if got := formatDebt(sites); got != want {
		t.Fatalf("DB syntax inventory=%q want=%q (including shadowed aliases, method expressions and wrapper signature)", got, want)
	}
}

func TestUnscopedASTMutations(t *testing.T) {
	collect := func(methods, body string) (debtSet, debtSet) {
		t.Helper()
		m, s, err := collectUnscoped(fstest.MapFS{
			"internal/storage/raw.go":      {Data: []byte("package storage\n" + methods)},
			"internal/service/use.go":      {Data: []byte("//go:build never_enabled\n\npackage service\nfunc f() {" + body + "}")},
			"internal/service/use_test.go": {Data: []byte("invalid excluded test")},
		}, parseDebt("Foo\nOld\n"))
		if err != nil {
			t.Fatal(err)
		}
		return m, s
	}
	baseline, baselineSites := collect("func (*StorageLayer) Old() {}", "x.Old()")
	for _, tc := range []struct {
		name, methods, body string
		added, stale        bool
		siteAdded, siteGone bool
	}{
		{"add Foo", "func (*StorageLayer) Old() {}; func (StorageLayer) Foo() {}", "x.Old(); _ = x.Foo", true, false, true, false},
		{"remove", "", "", false, true, false, true},
		{"replace same count", "func (StorageLayer) Foo() {}", "x.Foo()", true, true, true, true},
		{"duplicate reference", "func (*StorageLayer) Old() {}", "x.Old(); _ = x.Old", false, false, true, false},
		{"format only", "func (*StorageLayer) Old() {}", "\n\n x.Old( )\n", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			methods, sites := collect(tc.methods, tc.body)
			added, stale := compareDebt(methods, baseline)
			siteAdded, siteGone := compareDebt(sites, baselineSites)
			if (len(added) > 0) != tc.added || (len(stale) > 0) != tc.stale || (len(siteAdded) > 0) != tc.siteAdded || (len(siteGone) > 0) != tc.siteGone {
				t.Fatalf("method additions=%v stale=%v; inventory additions=%v stale=%v", added, stale, siteAdded, siteGone)
			}
		})
	}
}

func TestUnscopedSetComparison(t *testing.T) {
	for _, tc := range []struct {
		name, actual, golden, added, stale string
	}{
		{"Foo addition", "Foo\nOld\n", "Old\n", "Foo\n", ""},
		{"Foo removal", "Old\n", "Foo\nOld\n", "", "Foo\n"},
		{"same count replacement", "Foo\n", "Old\n", "Foo\n", "Old\n"},
		{"unchanged", "Foo\n", "Foo\n", "", ""},
		{"inventory addition", "a:f DB#1 DB#2\n", "a:f DB#1\n", "a:f DB#2\n", ""},
		{"inventory shrink", "a:f DB#1\n", "a:f DB#1 DB#2\n", "", "a:f DB#2\n"},
		{"inventory replacement", "a:g DB#1\n", "a:f DB#1\n", "a:g DB#1\n", "a:f DB#1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added, stale := compareDebt(parseDebt(tc.actual), parseDebt(tc.golden))
			if formatDebt(added) != tc.added || formatDebt(stale) != tc.stale {
				t.Fatalf("added=%q stale=%q; want added=%q stale=%q", formatDebt(added), formatDebt(stale), tc.added, tc.stale)
			}
		})
	}
}

func TestUnscopedCollectorParseError(t *testing.T) {
	_, _, err := collectUnscoped(fstest.MapFS{"internal/storage/bad.go": {Data: []byte("bad Go")}}, nil)
	if err == nil {
		t.Fatal("invalid production source must fail closed")
	}
}

// Nested agent checkouts are separate source trees, but other .claude source
// must still be scanned: the exclusion is not a blanket hidden-directory bypass.
func TestUnscopedInventoryNestedAgentWorktree(t *testing.T) {
	tree := fstest.MapFS{
		"internal/storage/a.go":                         {Data: []byte("package storage; type StorageLayer struct{}; func (*StorageLayer) Foo() {}")},
		".claude/worktrees/other/internal/service/a.go": {Data: []byte("not valid Go")},
		".claude/production.go":                         {Data: []byte("package service; func f() { x.Foo() }")},
	}
	_, sites, err := collectUnscoped(tree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatDebt(sites), ".claude/production.go:f Foo#1") {
		t.Fatalf("non-worktree source was omitted: %s", formatDebt(sites))
	}
}
