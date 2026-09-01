package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// sharedSessionStore models OpenCode's session store, which is NOT scoped to
// the server process that serves it: `GET /session` returns a store-wide
// listing, so every `opencode serve` started against the same workdir lists
// the same sessions. Two concurrently spawned tasks that share a workdir
// therefore each see the other's session.
type sharedSessionStore struct {
	mu       sync.Mutex
	sessions []opencodeSession
}

// add appends a session created at `created`. Its `updated` stamp is
// deliberately inverted relative to creation order, so a rule that ranks by
// recency picks the wrong session and the test says so.
func (s *sharedSessionStore) add(id string, created int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := opencodeSession{ID: id}
	sess.Time.Created = created
	sess.Time.Updated = 1_000_000 - created
	s.sessions = append(s.sessions, sess)
}

// serve starts one fake `opencode serve` process backed by the shared store.
func (s *sharedSessionStore) serve(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.sessions)
	}))
	t.Cleanup(srv.Close)
	return serverPortFromURL(t, srv.URL)
}

// TestDiscoverSessionID_ExcludeAloneCannotSeparateSharedWorkdir pins the
// defect in the raw heuristic: the per-spawn exclude set is a snapshot taken
// before the OTHER instance's session existed, so both instances see both new
// sessions and "newest wins" hands them the same ID. This is why claiming has
// to be runner-wide rather than per-spawn.
func TestDiscoverSessionID_ExcludeAloneCannotSeparateSharedWorkdir(t *testing.T) {
	store := &sharedSessionStore{}
	store.add("ses_pre", 1000)

	portA, portB := store.serve(t), store.serve(t)
	baselineA := baselineFor(t, portA)
	baselineB := baselineFor(t, portB)

	store.add("ses_a", 2000)
	store.add("ses_b", 3000)

	a, errA := discoverSessionID(portA, baselineA)
	b, errB := discoverSessionID(portB, baselineB)
	if errA != nil || errB != nil {
		t.Fatalf("discoverSessionID errors: %v / %v", errA, errB)
	}
	if a != b {
		t.Fatalf("expected the raw heuristic to collide, got %q and %q", a, b)
	}
}

// TestClaimDiscoveredSession_SharedWorkdir is the regression test for the bug:
// two tasks sharing one workdir must end up with distinct session IDs.
func TestClaimDiscoveredSession_SharedWorkdir(t *testing.T) {
	tests := []struct {
		name string
		// stagger reports whether task A's session already exists when task
		// B's server starts (and so lands in B's baseline).
		stagger bool
	}{
		{name: "simultaneous spawn", stagger: false},
		{name: "staggered spawn", stagger: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &sharedSessionStore{}
			store.add("ses_pre", 1000)

			var portA, portB int
			var baselineA, baselineB map[string]struct{}

			if tt.stagger {
				portA = store.serve(t)
				baselineA = baselineFor(t, portA)
				store.add("ses_a", 2000)

				portB = store.serve(t)
				baselineB = baselineFor(t, portB)
				store.add("ses_b", 3000)
			} else {
				portA, portB = store.serve(t), store.serve(t)
				baselineA = baselineFor(t, portA)
				baselineB = baselineFor(t, portB)
				store.add("ses_a", 2000)
				store.add("ses_b", 3000)
			}

			tr, pathA, pathB := runnerWithTwoTasksInOneWorkdir(t)

			a, err := tr.claimDiscoveredSession(pathA, portA, baselineA)
			if err != nil {
				t.Fatalf("claim A: %v", err)
			}
			b, err := tr.claimDiscoveredSession(pathB, portB, baselineB)
			if err != nil {
				t.Fatalf("claim B: %v", err)
			}

			if a == "" || b == "" {
				t.Fatalf("both tasks should get a session, got %q and %q", a, b)
			}
			if a == b {
				t.Fatalf("tasks sharing a workdir both claimed session %q", a)
			}
			assertRecordedSessions(t, tr, map[string]string{pathA: a, pathB: b})
		})
	}
}

// TestClaimDiscoveredSession_ConcurrentClaimsAreSerialized covers the race the
// per-spawn exclude set could never see: both discovery goroutines reading the
// session list at the same instant.
func TestClaimDiscoveredSession_ConcurrentClaimsAreSerialized(t *testing.T) {
	store := &sharedSessionStore{}
	store.add("ses_pre", 1000)

	portA, portB := store.serve(t), store.serve(t)
	baselineA := baselineFor(t, portA)
	baselineB := baselineFor(t, portB)

	store.add("ses_a", 2000)
	store.add("ses_b", 3000)

	tr, pathA, pathB := runnerWithTwoTasksInOneWorkdir(t)

	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	start := make(chan struct{})

	for i, c := range []struct {
		path     string
		port     int
		baseline map[string]struct{}
	}{
		{pathA, portA, baselineA},
		{pathB, portB, baselineB},
	} {
		wg.Add(1)
		go func(i int, path string, port int, baseline map[string]struct{}) {
			defer wg.Done()
			<-start
			results[i], errs[i] = tr.claimDiscoveredSession(path, port, baseline)
		}(i, c.path, c.port, c.baseline)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	if results[0] == results[1] {
		t.Fatalf("concurrent claims both took session %q", results[0])
	}
}

// TestClaimDiscoveredSession_ReclaimsOwnSession makes sure the claim set does
// not lock a task out of the session it already owns — re-discovery for the
// same task must be idempotent, not starved.
func TestClaimDiscoveredSession_ReclaimsOwnSession(t *testing.T) {
	store := &sharedSessionStore{}
	portA := store.serve(t)
	baseline := baselineFor(t, portA)
	store.add("ses_a", 2000)

	tr, pathA, _ := runnerWithTwoTasksInOneWorkdir(t)

	first, err := tr.claimDiscoveredSession(pathA, portA, baseline)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	second, err := tr.claimDiscoveredSession(pathA, portA, baseline)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if first != second {
		t.Fatalf("re-discovery for the same task changed session: %q then %q", first, second)
	}
}

// =============================================================================
// Pinned sessions (the primary fix — discovery never runs)
// =============================================================================

func TestCreateOpencodeSession_ReturnsID(t *testing.T) {
	var gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/session" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle = body["title"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ses_created","time":{"updated":42}}`))
	}))
	defer srv.Close()

	id, err := createOpencodeSession(serverPortFromURL(t, srv.URL), "Test Task abc123")
	if err != nil {
		t.Fatalf("createOpencodeSession: %v", err)
	}
	if id != "ses_created" {
		t.Fatalf("session id = %q, want ses_created", id)
	}
	if gotTitle != "Test Task abc123" {
		t.Fatalf("title = %q, want the task title so session lists stay readable", gotTitle)
	}
}

func TestCreateOpencodeSession_ErrorsOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := createOpencodeSession(serverPortFromURL(t, srv.URL), "t"); err == nil {
		t.Fatal("expected an error so the caller falls back to discovery")
	}
}

// TestSpawnHeadlessDirect_PinsSession checks the run process is told which
// session to use, which is what removes the guess entirely.
func TestSpawnHeadlessDirect_PinsSession(t *testing.T) {
	stateDir := t.TempDir()
	promptFile := filepath.Join(stateDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("do the thing"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	var gotArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.Command("/bin/echo", "spawned")
	}

	task := testResolvedTask("abc123")
	res, err := e.spawnHeadlessDirect(stateDir, "proj", task, promptFile, SpawnOptions{}, 4096, "ses_pinned")
	if err != nil {
		t.Fatalf("spawnHeadlessDirect: %v", err)
	}
	if res.SessionID != "ses_pinned" {
		t.Fatalf("SpawnResult.SessionID = %q, want ses_pinned", res.SessionID)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--session ses_pinned") {
		t.Fatalf("run args should pin the session, got: %s", joined)
	}
}

func TestSpawnHeadlessDirect_NoSessionFlagWithoutAttach(t *testing.T) {
	stateDir := t.TempDir()
	promptFile := filepath.Join(stateDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("do the thing"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	var gotArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.Command("/bin/echo", "spawned")
	}

	task := testResolvedTask("abc123")
	if _, err := e.spawnHeadlessDirect(stateDir, "proj", task, promptFile, SpawnOptions{}, 0, ""); err != nil {
		t.Fatalf("spawnHeadlessDirect: %v", err)
	}
	if strings.Contains(strings.Join(gotArgs, " "), "--session") {
		t.Fatalf("non-attached run must not pin a session, got: %v", gotArgs)
	}
}

// TestDiscoverAndSaveSession_PinnedSkipsDiscovery proves the heuristic is not
// consulted at all when the session was pinned at spawn: the fake server
// returns a decoy session that "newest wins" would otherwise take.
func TestDiscoverAndSaveSession_PinnedSkipsDiscovery(t *testing.T) {
	var sessionListCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" {
			sessionListCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"ses_decoy","time":{"updated":99999}}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tr, pathA, _ := runnerWithTwoTasksInOneWorkdir(t)
	tr.discoverAndSaveSession(pathA, 0, serverPortFromURL(t, srv.URL), nil, "ses_pinned")

	if sessionListCalls != 0 {
		t.Fatalf("pinned session should not query /session, got %d calls", sessionListCalls)
	}
	assertRecordedSessions(t, tr, map[string]string{pathA: "ses_pinned"})
}

// =============================================================================
// Helpers
// =============================================================================

func baselineFor(t *testing.T, port int) map[string]struct{} {
	t.Helper()
	ids, err := listSessionIDs(port)
	if err != nil {
		t.Fatalf("listSessionIDs(%d): %v", port, err)
	}
	return ids
}

// runnerWithTwoTasksInOneWorkdir builds a runner tracking two in-flight tasks
// that share a working directory — the shape that triggers the collision.
func runnerWithTwoTasksInOneWorkdir(t *testing.T) (*TaskRunner, string, string) {
	t.Helper()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(newMockClient(), newMockExecutor(), processMgr, newMockStateMgr())

	const workdir = "/repos/shared"
	pathA := "projects/proj-a/task/task-a.md"
	pathB := "projects/proj-a/task/task-b.md"

	for i, entry := range []struct{ id, path string }{{"task-a", pathA}, {"task-b", pathB}} {
		id, path := entry.id, entry.path
		if err := processMgr.Add(id, RunningTask{
			ID:           id,
			Path:         path,
			ProjectID:    "proj-a",
			Workdir:      workdir,
			ExecutorType: "opencode",
			InstanceID:   "inst_" + id,
		}, newMockProcess(1000+i)); err != nil {
			t.Fatalf("track %s: %v", id, err)
		}
	}
	return tr, pathA, pathB
}

func assertRecordedSessions(t *testing.T, tr *TaskRunner, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, info := range tr.processMgr.GetAll() {
		got[info.Task.Path] = info.Task.SessionID
	}
	for path, sessionID := range want {
		if got[path] != sessionID {
			t.Fatalf("task %s recorded session %q, want %q", path, got[path], sessionID)
		}
	}
}
