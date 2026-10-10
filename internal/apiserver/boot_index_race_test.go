package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// builtInGeneratedBy lists the generated_by markers of every built-in
// automation the boot path ensures when both toggles are on.
var builtInGeneratedBy = []string{
	"brain:builtin-feature-checkout",
	"brain:builtin-feature-checkout-simple",
	"brain:builtin-feature-delivery",
}

// seedSlowWalkFixture fills projects/ with many small non-markdown files. The
// boot scan's discovery walk must visit every one of them (they are inside a
// content root), but none is parsed, so the walk is long while the rest of the
// scan stays cheap. WalkDir is lexical: global/ — where the built-in
// automations live — is visited first, so built-ins written during the walk of
// projects/ are absent from the scan's disk snapshot.
func seedSlowWalkFixture(t *testing.T, root string, dirs, perDir int) {
	t.Helper()
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, "projects", "fixture", fmt.Sprintf("blob%03d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < perDir; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d.txt", f)), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// One real note so the content tree is not trivially empty.
	note := filepath.Join(root, "projects", "fixture", "note", "seed0001.md")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("---\ntitle: Seed\ntype: note\n---\n\nSeed body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// builtInFilesOnDisk returns generated_by → relative paths of built-in
// automation files found under global/.
func builtInFilesOnDisk(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "global"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range builtInGeneratedBy {
			if bytes.Contains(data, []byte("generated_by: "+marker+"\n")) ||
				bytes.Contains(data, []byte("generated_by: \""+marker+"\"\n")) {
				rel, _ := filepath.Rel(root, path)
				out[marker] = append(out[marker], filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// indexedPaths reopens the shared database after a joined boot and returns the
// local tenant's indexed note paths.
func indexedPaths(t *testing.T, root string) map[string]bool {
	t.Helper()
	ctx, err := backgroundTenantContext(context.Background(), tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, ".brain-data", "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	states, err := views.tenant.ListIndexedNoteStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool, len(states))
	for _, s := range states {
		out[s.Path] = true
	}
	return out
}

// bootAndJoin assembles the server once and runs cleanup, which joins the
// background boot scan, so the returned state is the scan's final result.
func bootAndJoin(t *testing.T, root string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := ServerOptions{
		Host: "127.0.0.1", BrainDir: root,
		Tenancy:         config.TenancyConfig{Mode: tenant.ModeSingle},
		FeatureCheckout: config.FeatureCheckoutConfig{Enabled: true},
		FeatureDelivery: config.FeatureDeliveryConfig{Enabled: true},
	}
	_, _, cleanup, err := buildHTTPHandler(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}

// TestBoot_BuiltInsWrittenDuringBootScanKeepIndexRowsAndNeverDuplicate
// reproduces brain task 0zr1gmn2: the built-in automations are ensured while
// the boot IndexChanged scan is still walking a large store. Their index rows
// must survive the scan, and a reboot must find them instead of writing a
// second set.
func TestBoot_BuiltInsWrittenDuringBootScanKeepIndexRowsAndNeverDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("large-fixture boot race reproduction")
	}
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(logger)

	root := t.TempDir()
	seedSlowWalkFixture(t, root, 4, 250)

	for boot := 1; boot <= 3; boot++ {
		bootAndJoin(t, root)
		onDisk := builtInFilesOnDisk(t, root)
		indexed := indexedPaths(t, root)
		for _, marker := range builtInGeneratedBy {
			paths := onDisk[marker]
			sort.Strings(paths)
			if len(paths) != 1 {
				t.Fatalf("boot %d: %s has %d files on disk %v, want exactly 1 (duplicate built-in)", boot, marker, len(paths), paths)
			}
			if !indexed[paths[0]] {
				t.Fatalf("boot %d: %s file %s exists on disk but its index row was lost", boot, marker, paths[0])
			}
		}
	}
}

// TestBoot_RESTSavesDuringBootScanKeepIndexRows drives ordinary REST saves
// through the real handler while the boot scan walks a large store. Every save
// must succeed (no "duplicate path" 500) and keep its index row once the scan
// has finished. projects/boot-race sorts before projects/fixture, so the walk
// has already passed the saves' directory while they land.
func TestBoot_RESTSavesDuringBootScanKeepIndexRows(t *testing.T) {
	if testing.Short() {
		t.Skip("large-fixture boot race reproduction")
	}
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(logger)

	root := t.TempDir()
	seedSlowWalkFixture(t, root, 4, 250)
	// Pre-create the saves' directory. The walk still passes it (empty) before
	// the saves land. Without it, concurrent first saves race to create it and
	// occasionally hit a separate, pre-existing tenantfs.canonical TOCTOU
	// (EvalSymlinks ENOENT, then Lstat sees the new dir) that 500s one save;
	// that is not the index race under test.
	if err := os.MkdirAll(filepath.Join(root, "projects", "boot-race", "summary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "127.0.0.1", BrainDir: root,
		Tenancy: config.TenancyConfig{Mode: tenant.ModeSingle}})
	if err != nil {
		t.Fatal(err)
	}

	const savers, perSaver = 4, 5
	var mu sync.Mutex
	var paths []string
	var wg sync.WaitGroup
	for w := 0; w < savers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perSaver; i++ {
				body := fmt.Sprintf(`{"type":"summary","title":"Boot race %d-%d","content":"saved during boot scan %d-%d","project":"boot-race"}`, w, i, w, i)
				r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/entries", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				if rec.Code != http.StatusCreated {
					t.Errorf("save %d-%d during boot scan: %d %s", w, i, rec.Code, rec.Body.String())
					continue
				}
				var created types.CreateEntryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Path == "" {
					t.Errorf("save %d-%d: bad response %s", w, i, rec.Body.String())
					continue
				}
				mu.Lock()
				paths = append(paths, created.Path)
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	cleanup() // joins the boot scan

	indexed := indexedPaths(t, root)
	if len(paths) != savers*perSaver {
		t.Fatalf("only %d of %d saves succeeded", len(paths), savers*perSaver)
	}
	for _, p := range paths {
		rel := strings.TrimSuffix(p, ".md") + ".md"
		if !indexed[rel] {
			t.Errorf("saved entry %s lost its index row to the boot scan", rel)
		}
	}
}

// syncBuffer collects slog output from the background boot scan goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// mdOnDisk returns every content-root markdown path under root.
func mdOnDisk(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, top := range []string{"projects", "global"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return filepath.SkipDir
				}
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				rel, _ := filepath.Rel(root, path)
				out[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func assertIndexMatchesDisk(t *testing.T, root, label string) {
	t.Helper()
	disk, indexed := mdOnDisk(t, root), indexedPaths(t, root)
	for p := range disk {
		if !indexed[p] {
			t.Errorf("%s: %s on disk but not indexed", label, p)
		}
	}
	for p := range indexed {
		if !disk[p] {
			t.Errorf("%s: %s indexed but not on disk", label, p)
		}
	}
}

// TestBoot_DeletesMovesAndRemovalsDuringBootScanNeverAbortIt is review
// follow-up O1 for brain task 0zr1gmn2. While the boot scan steps through
// a store, entries it already listed vanish: REST deletes and moves (the
// service removes the file before RemoveFile can take the index lock) and
// plain out-of-band rm. The scan must run to completion instead of logging
// "indexing failed"; REST results must be reflected exactly; a quiet reboot
// must leave index == disk with exactly one of each built-in.
func TestBoot_DeletesMovesAndRemovalsDuringBootScanNeverAbortIt(t *testing.T) {
	if testing.Short() {
		t.Skip("large-fixture boot race reproduction")
	}
	logs := &syncBuffer{}
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	defer slog.SetDefault(logger)

	root := t.TempDir()
	seedSlowWalkFixture(t, root, 2, 250)
	const seeds = 600
	seedPath := func(i int) string { return fmt.Sprintf("projects/zz-race/note/zr%06d.md", i) }
	for i := 0; i < seeds; i++ {
		full := filepath.Join(root, seedPath(i))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\ntitle: Seed %d\ntype: note\n---\n\nseed body %d\n", i, i)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts := ServerOptions{Host: "127.0.0.1", BrainDir: root,
		Tenancy:         config.TenancyConfig{Mode: tenant.ModeSingle},
		FeatureCheckout: config.FeatureCheckoutConfig{Enabled: true},
		FeatureDelivery: config.FeatureDeliveryConfig{Enabled: true},
	}
	boot := func() (http.Handler, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		h, _, cleanup, err := buildHTTPHandler(ctx, opts)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		return h, func() { cleanup(); cancel() }
	}
	scanOutcome := func(from int) string {
		s := logs.String()[from:]
		switch {
		case strings.Contains(s, "indexing failed"):
			return "failed"
		case strings.Contains(s, "indexing complete"):
			return "complete"
		}
		return ""
	}

	// Boot 1: index the seeds undisturbed.
	_, stop := boot()
	stop()
	assertIndexMatchesDisk(t, root, "boot 1")

	// Boot 2: disturb the scan. Seeds are partitioned so each is touched by
	// exactly one actor: i%4==0 out-of-band rm, 1 REST delete, 3 REST move.
	from := len(logs.String())
	h, stop := boot()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	var mu sync.Mutex
	deleted, movedFrom, movedTo, saved := []string{}, []string{}, []string{}, []string{}
	var wg sync.WaitGroup
	actor := func(rem int, act func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Walk the partition from the end, the part of the store the
			// scan steps through last, until the scan has finished.
			for i := seeds - 4 + rem; i >= 0 && scanOutcome(from) == ""; i -= 4 {
				act(i)
				time.Sleep(15 * time.Millisecond)
			}
		}()
	}
	actor(0, func(i int) { _ = os.Remove(filepath.Join(root, seedPath(i))) })
	actor(1, func(i int) {
		if w := do(http.MethodDelete, "/api/v1/entries/"+seedPath(i)+"?confirm=true", ""); w.Code != http.StatusNoContent {
			t.Errorf("delete %s: %d %s", seedPath(i), w.Code, w.Body.String())
			return
		}
		mu.Lock()
		deleted = append(deleted, seedPath(i))
		mu.Unlock()
	})
	actor(3, func(i int) {
		w := do(http.MethodPost, "/api/v1/entries/"+seedPath(i)+"/move", `{"project":"zz-moved"}`)
		var res types.MoveResult
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.To == "" {
			t.Errorf("move %s: %d %s", seedPath(i), w.Code, w.Body.String())
			return
		}
		mu.Lock()
		movedFrom, movedTo = append(movedFrom, seedPath(i)), append(movedTo, res.To)
		mu.Unlock()
	})
	actor(2, func(i int) {
		w := do(http.MethodPost, "/api/v1/entries", fmt.Sprintf(`{"type":"summary","title":"Boot2 save %d","content":"saved %d","project":"zz-saved"}`, i, i))
		var created types.CreateEntryResponse
		if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Path == "" {
			t.Errorf("save %d: %d %s", i, w.Code, w.Body.String())
			return
		}
		mu.Lock()
		saved = append(saved, created.Path)
		mu.Unlock()
	})
	wg.Wait()
	stop() // joins the boot scan
	if got := scanOutcome(from); got != "complete" {
		for _, line := range strings.Split(logs.String()[from:], "\n") {
			if strings.Contains(line, "indexing") {
				t.Log(line)
			}
		}
		t.Fatalf("boot 2 scan outcome = %q, want complete (a vanished file aborted the scan)", got)
	}
	indexed := indexedPaths(t, root)
	for _, p := range append(deleted, movedFrom...) {
		if indexed[p] {
			t.Errorf("boot 2: removed %s resurrected in the index", p)
		}
	}
	for _, p := range append(movedTo, saved...) {
		if !indexed[strings.TrimSuffix(p, ".md")+".md"] {
			t.Errorf("boot 2: written %s has no index row", p)
		}
	}
	if len(deleted) == 0 || len(movedFrom) == 0 || len(saved) == 0 {
		t.Fatalf("disturbance did not overlap the scan: %d deletes, %d moves, %d saves", len(deleted), len(movedFrom), len(saved))
	}

	// Boot 3: quiet reboot reconciles everything and keeps one of each built-in.
	from = len(logs.String())
	_, stop = boot()
	stop()
	if got := scanOutcome(from); got != "complete" {
		t.Fatalf("boot 3 scan outcome = %q", got)
	}
	assertIndexMatchesDisk(t, root, "boot 3")
	for marker, paths := range builtInFilesOnDisk(t, root) {
		if len(paths) != 1 {
			t.Errorf("boot 3: %s has %d files %v, want 1", marker, len(paths), paths)
		}
	}
}
