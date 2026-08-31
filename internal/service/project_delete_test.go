package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/types"
)

// seedEntry creates one entry of the given type in a project.
func seedEntry(t *testing.T, svc *BrainServiceImpl, project, entryType, title string) string {
	t.Helper()
	resp, err := svc.Save(context.Background(), types.CreateEntryRequest{
		Type:    entryType,
		Title:   title,
		Content: "content",
		Project: project,
	})
	if err != nil {
		t.Fatalf("seed %s %q: %v", entryType, title, err)
	}
	return resp.Path
}

func TestDeleteProject_RemovesEveryEntryTypeAndTheDirectory(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Deliberately mixed: the sidebar counts tasks, but a project wipe has
	// to take the notes and automations with it. A delete that only cleared
	// tasks would leave a project directory full of invisible entries.
	seedTasks(t, svc, "doomed", "feat", 3)
	note := seedEntry(t, svc, "doomed", "note", "A note")
	auto := seedEntry(t, svc, "doomed", "automation", "An automation")

	resp, err := svc.DeleteProject(ctx, "doomed")
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if resp.Deleted != 5 {
		t.Errorf("Deleted = %d, want 5 (3 tasks + note + automation)", resp.Deleted)
	}
	if resp.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (errors: %v)", resp.Failed, resp.Errors)
	}
	if !resp.DirectoryRemoved {
		t.Error("DirectoryRemoved = false; the project would keep showing up in ListProjects")
	}

	for _, p := range []string{note, auto} {
		if entry, err := svc.Recall(ctx, p); err == nil && entry != nil {
			t.Errorf("entry %s survived the wipe", p)
		}
	}
	if _, err := os.Stat(filepath.Join(brainDir, "projects", "doomed")); !os.IsNotExist(err) {
		t.Errorf("projects/doomed still on disk: %v", err)
	}
}

func TestDeleteProject_LeavesOtherProjectsAlone(t *testing.T) {
	// The failure mode that matters most: a prefix or project_id match that
	// is too wide takes a neighbouring project with it.
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "doomed", "feat", 2)
	keep := seedTasks(t, svc, "keeper", "feat", 2)

	if _, err := svc.DeleteProject(ctx, "doomed"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	for _, p := range keep {
		if entry, err := svc.Recall(ctx, p); err != nil || entry == nil {
			t.Errorf("entry %s in keeper was deleted; the match was too wide", p)
		}
	}
}

func TestDeleteProject_DoesNotMatchPrefixNeighbours(t *testing.T) {
	// "shop" must not swallow "shop-legacy". The path prefix carries the
	// trailing slash for exactly this reason.
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "shop", "feat", 1)
	keep := seedTasks(t, svc, "shop-legacy", "feat", 2)

	if _, err := svc.DeleteProject(ctx, "shop"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	for _, p := range keep {
		if entry, err := svc.Recall(ctx, p); err != nil || entry == nil {
			t.Errorf("entry %s in shop-legacy was deleted by a wipe of shop", p)
		}
	}
	if _, err := os.Stat(filepath.Join(brainDir, "projects", "shop-legacy")); err != nil {
		t.Errorf("shop-legacy directory removed by a wipe of shop: %v", err)
	}
}

func TestDeleteProject_UnderscoreInNameIsNotAWildcard(t *testing.T) {
	// SQL LIKE reads `_` as "any one character", so an unescaped prefix for
	// "a_b" also matches "axb". Harmless for a listing; unacceptable here.
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "a_b", "feat", 1)
	keep := seedTasks(t, svc, "axb", "feat", 1)

	if _, err := svc.DeleteProject(ctx, "a_b"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if entry, err := svc.Recall(ctx, keep[0]); err != nil || entry == nil {
		t.Error("project axb was deleted by a wipe of a_b; the LIKE prefix is unescaped")
	}
}

func TestDeleteProject_RemovesUnindexedFilesOnDisk(t *testing.T) {
	// A file that landed without an IndexFile call (a git pull into the
	// brain dir, a manual edit) is invisible to the index. Driving the wipe
	// from the index alone would leave it — and leave the directory.
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "mixed", "feat", 1)

	stray := filepath.Join(brainDir, "projects", "mixed", "note", "stray.md")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stray, []byte("# stray\n"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	if _, err := svc.DeleteProject(ctx, "mixed"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("unindexed file survived the wipe: %v", err)
	}
}

func TestDeleteProject_SweepsIndexRowsWhoseFileIsGone(t *testing.T) {
	// The mirror of the case above: the file vanished out of band but the
	// index row remains, so search and the link graph still serve it.
	svc, store, brainDir := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "ghost", "feat", 2)
	if err := os.Remove(filepath.Join(brainDir, filepath.FromSlash(paths[0]))); err != nil {
		t.Fatalf("remove file behind the index: %v", err)
	}

	if _, err := svc.DeleteProject(ctx, "ghost"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	remaining, err := store.ListProjectNotePaths(ctx, "ghost")
	if err != nil {
		t.Fatalf("ListProjectNotePaths: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("index rows survived the wipe: %v", remaining)
	}
}

func TestDeleteProject_PurgesProjectScopedState(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "stateful", "feat", 1)
	_ = paths

	// A claim and a pause dial: rows keyed by project_id that no
	// entry-level delete can reach.
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		 VALUES ('stateful', 't1', 'r1', 1, 9999999999)`); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO project_pause_state (project_id, tasks_paused, automations_paused, updated_at)
		 VALUES ('stateful', 1, 0, 1)`); err != nil {
		t.Fatalf("seed pause state: %v", err)
	}
	// A neighbour's claim, to pin that the purge is scoped.
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		 VALUES ('other', 't1', 'r1', 1, 9999999999)`); err != nil {
		t.Fatalf("seed neighbour claim: %v", err)
	}

	resp, err := svc.DeleteProject(ctx, "stateful")
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if resp.StateRowsRemoved["task_claims"] != 1 {
		t.Errorf("task_claims removed = %d, want 1", resp.StateRowsRemoved["task_claims"])
	}
	if resp.StateRowsRemoved["project_pause_state"] != 1 {
		t.Errorf("project_pause_state removed = %d, want 1",
			resp.StateRowsRemoved["project_pause_state"])
	}

	var others int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_claims WHERE project_id = 'other'`).Scan(&others); err != nil {
		t.Fatalf("count neighbour claims: %v", err)
	}
	if others != 1 {
		t.Errorf("neighbour project's claim count = %d, want 1", others)
	}
}

func TestDeleteProject_UnknownProjectIsNotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)

	_, err := svc.DeleteProject(context.Background(), "never-existed")
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_EmptyProjectIsDeletable(t *testing.T) {
	// A project whose entries are all gone still has a directory, and that
	// directory is the only reason ListProjects still names it. Refusing
	// here would make the leftover name permanent.
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	dir := filepath.Join(brainDir, "projects", "hollow", "task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resp, err := svc.DeleteProject(ctx, "hollow")
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if resp.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", resp.Deleted)
	}
	if !resp.DirectoryRemoved {
		t.Error("DirectoryRemoved = false; the empty project name would persist")
	}
}

func TestDeleteProject_RejectsIdsThatEscapeTheProjectsDir(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// A sibling of projects/ that must survive every attempt below.
	globalDir := filepath.Join(brainDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}

	for _, id := range []string{"", ".", "..", "../global", "a/b", `a\b`, "./x", "x/"} {
		t.Run("id="+id, func(t *testing.T) {
			if _, err := svc.DeleteProject(ctx, id); err == nil {
				t.Errorf("DeleteProject(%q) succeeded; it must be rejected", id)
			}
		})
	}

	if _, err := os.Stat(globalDir); err != nil {
		t.Errorf("global/ was removed by a traversal id: %v", err)
	}
}

func TestDeleteProject_PublishesOneProjectEvent(t *testing.T) {
	// One project.deleted, not one per entry: the summary is the signal a
	// UI subscribes to, and Delete already emits the per-entry events.
	svc, _, _ := newTestBrainService(t)
	bus := &recordingBus{}
	svc.bus = bus
	ctx := context.Background()

	seedTasks(t, svc, "eventful", "feat", 2)

	if _, err := svc.DeleteProject(ctx, "eventful"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	projectEvents := bus.ofType("project.deleted")
	if len(projectEvents) != 1 {
		t.Fatalf("project.deleted count = %d, want 1", len(projectEvents))
	}
	if got := projectEvents[0].ProjectID; got != "eventful" {
		t.Errorf("event ProjectID = %q, want %q", got, "eventful")
	}
	if got := projectEvents[0].Payload["deleted"]; got != 2 {
		t.Errorf("event payload deleted = %v, want 2", got)
	}
}

func TestValidateProjectID(t *testing.T) {
	valid := []string{"shop", "shop-legacy", "a_b", "Project.1", "0bd08245"}
	for _, id := range valid {
		if err := validateProjectID(id); err != nil {
			t.Errorf("validateProjectID(%q) = %v, want nil", id, err)
		}
	}
	invalid := []string{"", ".", "..", "../x", "a/b", `a\b`, "a/../b", "x/"}
	for _, id := range invalid {
		if err := validateProjectID(id); err == nil {
			t.Errorf("validateProjectID(%q) = nil, want an error", id)
		}
	}
}

func TestUnionPaths_DeduplicatesAndSorts(t *testing.T) {
	got := unionPaths(
		[]string{"projects/p/task/b.md", "projects/p/task/a.md", ""},
		[]string{"projects/p/task/b.md", "projects/p/note/c.md"},
	)
	want := []string{
		"projects/p/note/c.md",
		"projects/p/task/a.md",
		"projects/p/task/b.md",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unionPaths = %v, want %v", got, want)
	}
}

// recordingBus captures published events. Only Publish is exercised — the
// service never subscribes.
type recordingBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (b *recordingBus) Publish(e events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *recordingBus) Subscribe(events.EventType, events.Handler) events.Subscription {
	return nil
}

func (b *recordingBus) SubscribePattern(string, events.Handler) events.Subscription {
	return nil
}

func (b *recordingBus) Close() {}

func (b *recordingBus) ofType(t events.EventType) []events.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []events.Event
	for _, e := range b.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}
