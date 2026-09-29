package apiserver

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

func TestTenantSingleBootOneGraphAndCompatibility(t *testing.T) {
	// Structural count complements runtime compatibility: exactly one direct
	// constructor at boot, never inside a callback/request or the multi cache.
	src, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, decl := range src.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "buildHTTPHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if closure, ok := n.(*ast.FuncLit); ok {
				ast.Inspect(closure, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && (id.Name == "newTenantGraph" || id.Name == "newTenantGraphManager") {
						t.Error("graph constructed in boot callback")
					}
					return true
				})
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					if id.Name == "newTenantGraph" {
						count++
					}
					if id.Name == "newTenantGraphManager" {
						t.Error("single boot uses cache")
					}
				}
			}
			return true
		})
	}
	if count != 1 {
		t.Fatalf("single boot constructors=%d want1", count)
	}
	root := t.TempDir()
	start := time.Now()
	h, dbPath, cleanup, err := buildHTTPHandler(context.Background(), ServerOptions{Host: "127.0.0.1", BrainDir: root, Tenancy: config.TenancyConfig{Mode: tenant.ModeSingle}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	t.Logf("fresh single buildHTTPHandler ready=%s (scan asynchronous; includes DB/schema/root/router/workers)", time.Since(start))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/entries/", strings.NewReader(`{"type":"report","title":"Single fixture","content":"compatibilitymarker","project":"shared"}`)))
	if w.Code != 201 {
		t.Fatalf("single write: %d %s", w.Code, w.Body.String())
	}
	var created types.BrainEntry
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// Existing create envelope can be inspected without guessing the entry ID.
	var envelope struct {
		Entry types.BrainEntry `json:"entry"`
	}
	if created.Path == "" {
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		created = envelope.Entry
	}
	if created.Path == "" {
		t.Fatalf("missing created path: %s", w.Body.String())
	}
	for i := 0; i < 100; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/entries/"+created.Path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "compatibilitymarker") {
			t.Fatalf("repeat %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, created.Path)); err != nil {
		t.Fatal("legacy markdown location", err)
	}
	if dbPath != filepath.Join(root, ".brain-data", "brain.db") {
		t.Fatal("database relocated", dbPath)
	}
	cleanup()
	h, again, cleanup, err := buildHTTPHandler(context.Background(), ServerOptions{Host: "127.0.0.1", BrainDir: root})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if again != dbPath {
		t.Fatal("database moved across restart")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/entries/"+created.Path, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "compatibilitymarker") {
		t.Fatalf("restart: %d %s", w.Code, w.Body.String())
	}
}

// Meaningful extraction baseline: graph-only single-root cost vs whole current
// boot composition (including open/schema/root/router/worker admission). This is
// NOT a claim of bit-identical timing against the pre-extraction binary.
func BenchmarkTenantSingleConstruction(b *testing.B) {
	b.Run("graph-only", func(b *testing.B) {
		root := b.TempDir()
		views, err := openSingleModeStorage(context.Background(), tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
		if err != nil {
			b.Fatal(err)
		}
		defer views.close()
		if _, err := views.roots.ProvisionLocal(context.Background(), root, filepath.Join(root, "attachments")); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			g, err := newTenantGraph(context.Background(), views.tenant, views.roots, config.Config{}, graphIdentity{})
			if err != nil {
				b.Fatal(err)
			}
			g.Close()
		}
	})
	b.Run("boot-existing-empty", func(b *testing.B) {
		root := b.TempDir()
		opts := ServerOptions{Host: "127.0.0.1", BrainDir: root}
		_, _, cleanup, err := buildHTTPHandler(context.Background(), opts)
		if err != nil {
			b.Fatal(err)
		}
		cleanup()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, cleanup, err := buildHTTPHandler(context.Background(), opts)
			if err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			cleanup()
			b.StartTimer()
		}
	})
}
