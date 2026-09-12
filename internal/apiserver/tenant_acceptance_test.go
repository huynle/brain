package apiserver

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/internal/types"
)

type graphFixture struct {
	db    *sql.DB
	owner *storage.StorageLayer
	roots *tenantfs.Resolver
	ids   []tenant.ID
}

func newGraphFixture(t testing.TB, n int) *graphFixture {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "graph-fixture.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	owner, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := tenantfs.New(owner, root) // real registry, test-only owner access
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(context.Background(), root, filepath.Join(root, "attachments")); err != nil {
		t.Fatal(err)
	}
	f := &graphFixture{db: db, owner: owner, roots: roots, ids: []tenant.ID{tenant.Local}}
	for i := 1; i < n; i++ {
		id := tenant.MustParse(fmt.Sprintf("tenant-%03d", i))
		f.ids = append(f.ids, id)
	}
	for _, id := range f.ids[:1] {
		mapping, err := roots.Lookup(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(mapping.BlobAbsolute, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Existing live v28 owner is retained; only the test child can invoke the
	// private migration. Never reopen this v29 file with a public constructor.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "../storage", "-run", "^TestTenantGraphFixtureStage$", "-count=1")
	cmd.Env = append(os.Environ(), "BRAIN_GRAPH_FIXTURE="+path, fmt.Sprintf("BRAIN_GRAPH_TENANTS=%d", n))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture: %v\n%s", err, out)
	}
	for _, id := range f.ids[1:] {
		mapping, err := roots.Provision(context.Background(), id, tenantfs.Overrides{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(mapping.BlobAbsolute, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version != 29 || storage.CurrentSchemaVersion != 28 {
		t.Fatalf("staging guard: %d %v", version, err)
	}
	return f
}

func (f *graphFixture) graph(t testing.TB, id tenant.ID) *tenantGraph {
	t.Helper()
	s, err := f.owner.ForTenant(id)
	if err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(context.Background(), s, f.roots, config.Config{}, graphIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

const collisionPath = "projects/shared/note/same0001.md"
const targetPath = "projects/shared/note/same0002.md"

func writeGraphNote(t testing.TB, f *graphFixture, id tenant.ID, path, body string) {
	t.Helper()
	m, err := f.roots.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(m.BrainAbsolute, path)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\ntitle: "+body+"\ntype: note\nprojectId: shared\nstatus: active\n---\n\n"+body+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func seedGraphContent(t testing.TB, f *graphFixture, id tenant.ID) types.Attachment {
	t.Helper()
	g := f.graph(t, id)
	defer g.Close()
	for _, path := range []string{targetPath, collisionPath} {
		body := id.String() + " secretmarker"
		if path == collisionPath {
			body += " [target](same0002.md)"
		}
		writeGraphNote(t, f, id, path, body)
		if err := g.indexer.IndexFile(path); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := f.owner.ForTenant(id)
	blobs, err := blobstore.NewTenantFilesystemStore(f.roots, id, 4096)
	if err != nil {
		t.Fatal(err)
	}
	as := service.NewAttachmentService(s, blobs, g.brain, 4096)
	// Equal bytes have equal digests but distinct physical objects and integer IDs.
	a, err := as.Create(context.Background(), "shared", types.CreateAttachmentRequest{Filename: id.String() + ".txt", Size: 12}, strings.NewReader("shared bytes"))
	if err != nil {
		t.Fatal(err)
	}
	// Use migrated storage to seed the relationship: generic service writes may
	// reach unmigrated task metadata and are deliberately not HTTP-supported.
	note, err := s.GetNoteByPath(context.Background(), collisionPath)
	if err != nil || note == nil {
		t.Fatal("missing seeded note", err)
	}
	if _, err := f.db.Exec("INSERT INTO entry_attachments(tenant_id,note_id,attachment_id,role) VALUES(?,?,?,'source')", id.String(), note.ID, a.Attachment.ID); err != nil {
		t.Fatal(err)
	}
	mapping, err := f.roots.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(mapping.BrainAbsolute, collisionPath)
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "status: active", fmt.Sprintf("status: active\nattachments:\n  - id: %q\n    filename: %s.txt\n    role: source", a.Attachment.ID, id), 1))
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := g.indexer.IndexFile(collisionPath); err != nil {
		t.Fatal(err)
	}
	if _, err := as.StoreDerivedText(context.Background(), "shared", a.Attachment.ID, types.AttachmentDerivedText{Status: "ready", Text: id.String() + " derivedsecret"}); err != nil {
		t.Fatal(err)
	}
	return a.Attachment
}

func graphRequest(h http.Handler, id tenant.ID, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(tenant.Into(r.Context(), id))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTenantAcceptanceConcurrentHTTP(t *testing.T) {
	f := newGraphFixture(t, 2)
	a, b := seedGraphContent(t, f, f.ids[0]), seedGraphContent(t, f, f.ids[1])
	if a.ID == b.ID || a.SHA256 != b.SHA256 {
		t.Fatal("integer attachment IDs are global; digest must collide")
	}
	var builds atomic.Int32
	factory := tenantHTTPFactory(f.owner.ForTenant, f.roots, config.Config{})
	m, err := newTenantGraphManager(2, activeGraphAuthority, func(ctx context.Context, id tenant.ID) (graphResource, error) { builds.Add(1); return factory(ctx, id) })
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownGraphs(t, m)
	h := tenantWorkloadHTTP(m)
	servers := make([]*httptest.Server, len(f.ids))
	for i, id := range f.ids {
		// Listener-specific binding is fixture authority, never a trusted header.
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(tenant.Into(r.Context(), id)))
		}))
		defer servers[i].Close()
	}
	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id, foreign := f.ids[i%2], f.ids[1-i%2]
				own, other := a, b
				if i%2 == 1 {
					own, other = b, a
				}
				for _, tc := range []struct {
					method, path, body, want string
					code                     int
				}{
					{"GET", "/api/v1/entries/same0001", "", id.String() + " secretmarker", 200},
					{"GET", "/api/v1/entries/" + collisionPath, "", id.String() + " secretmarker", 200},
					{"GET", "/api/v1/entries/?project=shared", "", id.String() + " secretmarker", 200},
					{"POST", "/api/v1/search", `{"query":"secretmarker","project":"shared"}`, id.String() + " secretmarker", 200},
					{"GET", "/api/v1/entries/same0001/outlinks", "", id.String() + " secretmarker", 200},
					{"GET", "/api/v1/entries/same0002/backlinks", "", id.String() + " secretmarker", 200},
					{"GET", "/api/v1/entries/same0001/attachments?project_id=shared", "", id.String() + ".txt", 200},
					{"GET", "/api/v1/attachments/?project_id=shared", "", id.String() + ".txt", 200},
					{"GET", "/api/v1/attachments/" + own.ID + "?project_id=shared", "", id.String() + ".txt", 200},
					{"GET", "/api/v1/attachments/" + own.ID + "/content?project_id=shared", "", "shared bytes", 200},
					{"GET", "/api/v1/attachments/" + own.ID + "/text?project_id=shared", "", id.String() + " derivedsecret", 200},
					{"GET", "/api/v1/attachments/" + other.ID + "?project_id=shared", "", "", 404},
					{"GET", "/api/v1/attachments/" + other.ID + "/content?project_id=shared", "", "", 404},
					{"GET", "/api/v1/attachments/" + other.ID + "/text?project_id=shared", "", "", 404},
				} {
					r, err := http.NewRequest(tc.method, servers[i%2].URL+tc.path, strings.NewReader(tc.body))
					if err != nil {
						t.Error(err)
						return
					}
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("X-Brain-Tenant", foreign.String()) // cannot override fixture scope
					response, err := servers[i%2].Client().Do(r)
					if err != nil {
						t.Error(err)
						return
					}
					data, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil {
						t.Error(err)
						return
					}
					text := string(data)
					if response.StatusCode != tc.code || !strings.Contains(text, tc.want) || strings.Contains(text, foreign.String()+" secretmarker") || strings.Contains(text, foreign.String()+" derivedsecret") || strings.Contains(text, foreign.String()+".txt") {
						t.Errorf("%s %s: %d %s", id, tc.path, response.StatusCode, text)
					}
				}
			}(worker)
		}
		wg.Wait()
		// Force reconstruction, not merely a cache-hit test.
		m.Invalidate(f.ids[0])
		m.Invalidate(f.ids[1])
	}
	if builds.Load() != 6 {
		t.Fatalf("builds=%d want 2 per round", builds.Load())
	}
	for _, id := range f.ids {
		for _, path := range []string{"/api/v1/entries/", "/api/v1/entries/bulk-update", "/api/v1/attachments/", "/api/v1/attachments/1/extract", "/api/v1/assistant/chat", "/api/v1/assistant/jobs", "/api/v1/assistant/voice", "/api/v1/push/subscribe", "/api/v1/tasks/shared/run", "/api/v1/config", "/api/v1/tokens/", "/mcp"} {
			w := graphRequest(h, id, "POST", path, `{"title":"must not persist","type":"task","project":"shared"}`)
			if w.Code != 501 {
				t.Errorf("unsupported %s: %d %s", path, w.Code, w.Body.String())
			}
		}
		for _, method := range []string{"PATCH", "PUT", "DELETE"} {
			for _, path := range []string{"/api/v1/entries/" + collisionPath, "/api/v1/attachments/" + a.ID + "?project_id=shared", "/api/v1/config"} {
				w := graphRequest(h, id, method, path, `{"title":"must not persist"}`)
				if w.Code != 501 {
					t.Errorf("unsupported %s %s: %d %s", method, path, w.Code, w.Body.String())
				}
			}
		}
		var count int
		if err := f.db.QueryRow("SELECT count(*) FROM notes WHERE tenant_id=?", id.String()).Scan(&count); err != nil || count != 2 {
			t.Fatalf("unsupported mutation reached persistence: %d %v", count, err)
		}
		w := graphRequest(h, id, "GET", "/api/v1/entries/same0001", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), id.String()+" secretmarker") || strings.Contains(w.Body.String(), "must not persist") {
			t.Fatalf("unsupported entry write changed content: %d %s", w.Code, w.Body.String())
		}
		attachment := a
		if id != tenant.Local {
			attachment = b
		}
		w = graphRequest(h, id, "GET", "/api/v1/attachments/"+attachment.ID+"/content?project_id=shared", "")
		if w.Code != 200 || w.Body.String() != "shared bytes" {
			t.Fatalf("unsupported attachment mutation: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestTenantAcceptanceLegacyParentAndReusedPaths(t *testing.T) {
	f := newGraphFixture(t, 2)
	a, b := seedGraphContent(t, f, f.ids[0]), seedGraphContent(t, f, f.ids[1])
	local := f.graph(t, tenant.Local)
	defer local.Close()
	foreign, _ := f.roots.Lookup(context.Background(), f.ids[1])
	before, err := os.ReadFile(filepath.Join(foreign.BrainAbsolute, collisionPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := local.indexer.IndexFile("tenants/tenant-001/" + collisionPath); err == nil {
		t.Fatal("direct foreign indexing admitted")
	}
	if _, err := local.brain.Recall(context.Background(), "tenants/tenant-001/"+collisionPath); err == nil {
		t.Fatal("foreign file read admitted")
	}
	for _, scan := range []func() error{
		func() error {
			r, e := local.indexer.IndexChanged()
			if e == nil && len(r.Errors) > 0 {
				return fmt.Errorf("%v", r.Errors)
			}
			return e
		},
		func() error {
			r, e := local.indexer.RebuildAll()
			if e == nil && len(r.Errors) > 0 {
				return fmt.Errorf("%v", r.Errors)
			}
			return e
		},
	} {
		if err := scan(); err != nil {
			t.Fatal(err)
		}
	}
	list, err := local.brain.List(context.Background(), types.ListEntriesRequest{})
	if err != nil || len(list.Entries) != 2 {
		t.Fatalf("local scan %+v %v", list, err)
	}
	for _, entry := range list.Entries {
		if strings.Contains(entry.Title, "tenant-001") {
			t.Fatal("foreign index observation")
		}
	}
	// Delete/recreate a logical ID/path with new content, rebuild graph and index.
	s, _ := f.owner.ForTenant(tenant.Local)
	if _, err := s.DeleteNote(context.Background(), collisionPath); err != nil {
		t.Fatal(err)
	}
	writeGraphNote(t, f, tenant.Local, collisionPath, "local replacementsecret")
	if err := local.indexer.IndexFile(collisionPath); err != nil {
		t.Fatal(err)
	}
	local.Close()
	next := f.graph(t, tenant.Local)
	defer next.Close()
	w := graphRequest(tenantContentRoutes(next.handler), tenant.Local, "GET", "/api/v1/entries/same0001", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "replacementsecret") {
		t.Fatalf("reused path: %d %s", w.Code, w.Body.String())
	}
	other := f.graph(t, f.ids[1])
	defer other.Close()
	w = graphRequest(tenantContentRoutes(other.handler), f.ids[1], "GET", "/api/v1/entries/same0001", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "tenant-001 secretmarker") {
		t.Fatalf("foreign index changed: %d %s", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(filepath.Join(foreign.BrainAbsolute, collisionPath))
	if err != nil || string(before) != string(after) {
		t.Fatal("foreign file changed", err)
	}
	// Same digest is stored independently in both legacy and tenant CAS layouts.
	paths := []string{}
	for i, att := range []types.Attachment{a, b} {
		mapping, _ := f.roots.Lookup(context.Background(), f.ids[i])
		p := filepath.Join(mapping.BlobAbsolute, att.SHA256[:2], att.SHA256)
		if i == 0 {
			p = filepath.Join(mapping.BlobAbsolute, att.SHA256[:2], att.SHA256[2:4], att.SHA256)
		}
		data, err := os.ReadFile(p)
		if err != nil || string(data) != "shared bytes" {
			t.Fatal("physical CAS", err)
		}
		paths = append(paths, p)
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	w = graphRequest(tenantContentRoutes(other.handler), f.ids[1], "GET", "/api/v1/attachments/"+b.ID+"/content?project_id=shared", "")
	if w.Code != 200 || w.Body.String() != "shared bytes" {
		t.Fatalf("foreign CAS affected: %d %s", w.Code, w.Body.String())
	}
}

// Gate only socket output, not persistence or services. The real handler has
// executed when entered closes. This proves lease drain, NOT P6 output fencing.
type heldGraphWriter struct {
	http.ResponseWriter
	entered, finish chan struct{}
	once            sync.Once
}

func (w *heldGraphWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.finish })
	return w.ResponseWriter.Write(p)
}

func TestTenantAcceptanceHeldHTTPAndSuspension(t *testing.T) {
	f := newGraphFixture(t, 2)
	seedGraphContent(t, f, f.ids[0])
	seedGraphContent(t, f, f.ids[1])
	var mu sync.Mutex
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	generation := uint64(1)
	authority := func(ctx context.Context, id tenant.ID) (graphPermit, error) {
		mu.Lock()
		defer mu.Unlock()
		var status string
		if err := f.db.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=?", id.String()).Scan(&status); err != nil || status != "active" {
			return graphPermit{}, errGraphUnavailable
		}
		return graphPermit{Generation: generation, Lifetime: life}, nil
	}
	var builds atomic.Int32
	factory := tenantHTTPFactory(f.owner.ForTenant, f.roots, config.Config{})
	m, _ := newTenantGraphManager(1, authority, func(ctx context.Context, id tenant.ID) (graphResource, error) { builds.Add(1); return factory(ctx, id) })
	defer shutdownGraphs(t, m)
	h := tenantWorkloadHTTP(m)
	entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(finish) }) }
	defer release()
	w := httptest.NewRecorder()
	go func() {
		defer close(done)
		r := httptest.NewRequest("GET", "/api/v1/entries/same0001", nil)
		h.ServeHTTP(&heldGraphWriter{ResponseWriter: w, entered: entered, finish: finish}, r.WithContext(tenant.Into(r.Context(), tenant.Local)))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real response not reached")
	}
	m.Invalidate(tenant.Local)
	m.mu.Lock()
	slot := m.entries[tenant.Local]
	retained := slot != nil && slot.refs == 1 && slot.retiring && !slot.closing
	m.mu.Unlock()
	if !retained {
		t.Error("retired graph not retained by request")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if l, err := m.Acquire(ctx, f.ids[1]); err == nil {
		l.Release()
		t.Error("capacity exceeded during held eviction")
	}
	stop()
	if builds.Load() != 1 {
		t.Error("second graph built before drain")
	}
	release()
	<-done
	if w.Code != 200 || !strings.Contains(w.Body.String(), "local secretmarker") {
		t.Fatalf("held response: %d %s", w.Code, w.Body.String())
	}
	if w := graphRequest(h, f.ids[1], "GET", "/api/v1/entries/same0001", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	mu.Lock()
	_, err := f.db.Exec("UPDATE tenants SET status='suspended' WHERE id='tenant-001'")
	cancel()
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if w := graphRequest(h, f.ids[1], "GET", "/api/v1/entries/same0001", ""); w.Code != 503 || strings.Contains(w.Body.String(), "secretmarker") {
		t.Fatalf("suspended admission: %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	_, err = f.db.Exec("UPDATE tenants SET status='active' WHERE id='tenant-001'")
	life, cancel = context.WithCancel(context.Background())
	generation++
	mu.Unlock()
	defer cancel()
	if err != nil {
		t.Fatal(err)
	}
	if w := graphRequest(h, f.ids[1], "GET", "/api/v1/entries/same0001", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "tenant-001 secretmarker") {
		t.Fatalf("reactivated: %d %s", w.Code, w.Body.String())
	}
	if builds.Load() != 3 {
		t.Fatalf("builds=%d want3", builds.Load())
	}
}

var _ io.Writer = (*heldGraphWriter)(nil)
