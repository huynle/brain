package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

const mgrToken = "TOKEN-mgr-4417"

// refStore answers the reference finder from a fixed table.
type refStore struct {
	mu     sync.Mutex
	byName map[string][]Reference
	err    error
}

func (r *refStore) find(_ context.Context, name string) ([]Reference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return append([]Reference(nil), r.byName[name]...), nil
}

func (r *refStore) set(name string, refs ...Reference) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = make(map[string][]Reference)
	}
	r.byName[name] = refs
}

// mgrEnv is a manager over a real registry and poller, with a TLS feed server
// on loopback. Names are resolved by hook: example.com is the loopback server
// (a valid TLS name for its certificate), and private.example is a private
// address.
type mgrEnv struct {
	t       *testing.T
	dataDir string
	srv     *feedServer
	port    string
	clock   *fakeClock
	logs    *syncBuffer
	refs    *refStore
	reg     *Registry
	poller  *Poller
	mgr     *Manager
}

var mgrHostOf = map[string][]string{
	"example.com":     {"127.0.0.1"},
	"private.example": {"10.0.0.9"},
	"lan.example":     {"192.168.1.20"},
}

func newMgrHooks(t *testing.T, srv *feedServer) runtimeHooks {
	t.Helper()
	loopback := netip.MustParseAddr("127.0.0.1")
	return runtimeHooks{
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			raw, ok := mgrHostOf[host]
			if !ok {
				return nil, fmt.Errorf("no such host")
			}
			out := make([]netip.Addr, 0, len(raw))
			for _, s := range raw {
				out = append(out, netip.MustParseAddr(s))
			}
			return out, nil
		},
		allowed: func(a netip.Addr) bool { return a == loopback },
		tls:     srv.srv.Client().Transport.(*http.Transport).TLSClientConfig,
	}
}

func newMgrEnv(t *testing.T, cfgs map[string]config.CalendarConfig, opts ManagerOptions) *mgrEnv {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	now := clock.Now()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "std", title: "Standup", start: now.Add(time.Hour)})))
	env := &mgrEnv{
		t:       t,
		dataDir: t.TempDir(),
		srv:     srv,
		port:    portOf(t, srv.srv.Listener.Addr().String()),
		clock:   clock,
		logs:    &syncBuffer{},
		refs:    &refStore{},
	}
	env.open(cfgs, opts)
	return env
}

// open builds a fresh registry, poller and manager over the same data
// directory, as a restart does.
func (env *mgrEnv) open(cfgs map[string]config.CalendarConfig, opts ManagerOptions) {
	env.t.Helper()
	reg, err := NewRegistry(cfgs)
	if err != nil {
		env.t.Fatalf("NewRegistry: %v", err)
	}
	poller, err := NewPoller(reg, nil, env.dataDir, PollerOptions{
		Clock:  env.clock.Now,
		Logger: slog.New(slog.NewTextHandler(env.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		env.t.Fatalf("NewPoller: %v", err)
	}
	if opts.References == nil {
		opts.References = env.refs.find
	}
	if opts.Clock == nil {
		opts.Clock = env.clock.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(env.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	opts.hooks = newMgrHooks(env.t, env.srv)
	mgr, err := NewManager(reg, poller, env.dataDir, opts)
	if err != nil {
		env.t.Fatalf("NewManager: %v", err)
	}
	env.reg, env.poller, env.mgr = reg, poller, mgr
}

// addr is an https address on the loopback server, under the example.com name.
func (env *mgrEnv) addr(path string) string {
	return "https://example.com:" + env.port + path
}

func (env *mgrEnv) sourceFile(name string) string {
	return filepath.Join(env.dataDir, "calendars", "sources", name+".json")
}

func (env *mgrEnv) snapshotFile(name string) string {
	return filepath.Join(env.dataDir, "calendars", name+".json")
}

func (env *mgrEnv) add(name, address string) Status {
	env.t.Helper()
	st, err := env.mgr.Add(context.Background(), AddRequest{Name: name, URL: address}, "huy")
	if err != nil {
		env.t.Fatalf("Add(%s): %v", name, err)
	}
	return st
}

func mgrConfigs() map[string]config.CalendarConfig {
	return map[string]config.CalendarConfig{
		"team": {Type: KindICS, URLEnv: "TEAM_CAL_URL", Poll: "2m"},
		"xnys": {Type: KindBuiltin, Market: "XNYS"},
	}
}

func TestNewManagerRejectsNilRegistryOrPoller(t *testing.T) {
	reg, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPoller(reg, nil, t.TempDir(), PollerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(nil, p, t.TempDir(), ManagerOptions{}); err == nil {
		t.Fatal("nil registry accepted")
	}
	if _, err := NewManager(reg, nil, t.TempDir(), ManagerOptions{}); err == nil {
		t.Fatal("nil poller accepted")
	}
	if _, err := NewManager(reg, p, t.TempDir(), ManagerOptions{MaxSources: -1}); err == nil {
		t.Fatal("negative MaxSources accepted")
	}
}

func TestAddPublishesStatusPersistsAndFetchesOnce(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	st := env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))

	if st.Origin != OriginRuntime || st.Kind != KindICS || st.Poll != 5*time.Minute {
		t.Fatalf("status: %+v want runtime ics 5m", st)
	}
	if st.EventCount != 1 || st.LastSuccess.IsZero() || st.LastError != "" {
		t.Fatalf("first snapshot not published: %+v", st)
	}
	if _, err := os.Stat(env.sourceFile("personal")); err != nil {
		t.Fatalf("source file not written: %v", err)
	}
	if hits := env.srv.hits.Load(); hits != 1 {
		t.Fatalf("add fetched %d times, want exactly 1 validation fetch", hits)
	}
	if got := env.srv.lastPath(); got != "/feed/"+mgrToken+"/cal.ics" {
		t.Fatalf("validation fetched %q", got)
	}
}

func TestAddedSourceSurvivesRestartServedFromCache(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	hits := env.srv.hits.Load()

	env.open(mgrConfigs(), ManagerOptions{})
	st := statusNamed(t, env.reg, "personal")
	if st.Origin != OriginRuntime || st.Poll != 5*time.Minute || st.EventCount != 1 {
		t.Fatalf("reloaded status: %+v", st)
	}
	if env.srv.hits.Load() != hits {
		t.Fatal("restart fetched the feed; the cached snapshot should serve first")
	}
}

func TestNewManagerLoadsRuntimeSourcesAndSkipsCollisionsAndBadFiles(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	now := env.clock.Now()
	store := newSourceStore(env.dataDir)
	// A runtime file named like a config source must never shadow it.
	if err := store.save(sourceRecord{Name: "team", URL: env.addr("/feed/SHADOW/cal.ics"), Poll: time.Minute, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.dataDir, "calendars", "sources", "broken.json"), []byte(`{"url": "`+mgrToken+``), 0o600); err != nil {
		t.Fatal(err)
	}

	env.open(mgrConfigs(), ManagerOptions{})
	if origin, _ := env.reg.Origin("team"); origin != OriginConfig {
		t.Fatalf("config source team replaced by a runtime file: origin %q", origin)
	}
	if origin, ok := env.reg.Origin("personal"); !ok || origin != OriginRuntime {
		t.Fatalf("runtime source personal not loaded: %q %v", origin, ok)
	}
	if _, ok := env.reg.Kind("broken"); ok {
		t.Fatal("corrupt source file was loaded")
	}
	logs := env.logs.String()
	for _, want := range []string{"broken.json", "team"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("skipped file %q not reported in logs:\n%s", want, logs)
		}
	}
}

func TestNewManagerHonoursTheSourceLimitOnLoad(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{MaxSources: 1})
	now := env.clock.Now()
	store := newSourceStore(env.dataDir)
	for _, n := range []string{"alpha", "beta"} {
		if err := store.save(sourceRecord{Name: n, URL: env.addr("/feed/" + n + "/cal.ics"), Poll: 5 * time.Minute, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	env.open(mgrConfigs(), ManagerOptions{MaxSources: 1})
	loaded := 0
	for _, n := range []string{"alpha", "beta"} {
		if _, ok := env.reg.Kind(n); ok {
			loaded++
		}
	}
	if loaded != 1 {
		t.Fatalf("loaded %d runtime sources over a limit of 1", loaded)
	}
}

func TestAddValidationFailureSavesNothingAndFetchesNothing(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	hits := env.srv.hits.Load()
	good := env.addr("/feed/" + mgrToken + "/cal.ics")
	tests := []struct {
		name    string
		req     AddRequest
		wantErr error
	}{
		{"upper case name", AddRequest{Name: "Personal", URL: good}, ErrInvalidName},
		{"path name", AddRequest{Name: "../escape", URL: good}, ErrInvalidName},
		{"empty name", AddRequest{Name: "", URL: good}, ErrInvalidName},
		{"name too long", AddRequest{Name: strings.Repeat("a", 65), URL: good}, ErrInvalidName},
		{"poll below a minute", AddRequest{Name: "p1", URL: good, Poll: 30 * time.Second}, ErrInvalidPoll},
		{"negative poll", AddRequest{Name: "p2", URL: good, Poll: -time.Minute}, ErrInvalidPoll},
		{"plain http", AddRequest{Name: "p3", URL: "http://example.com/feed/" + mgrToken}, ErrRejectedURL},
		{"user info", AddRequest{Name: "p4", URL: "https://user:" + mgrToken + "@example.com:" + env.port + "/x"}, ErrRejectedURL},
		{"no host", AddRequest{Name: "p5", URL: "https:///feed/" + mgrToken}, ErrRejectedURL},
		{"unparseable", AddRequest{Name: "p6", URL: "https://exa mple.com/feed/" + mgrToken}, ErrRejectedURL},
		{"too long", AddRequest{Name: "p7", URL: "https://example.com/" + strings.Repeat("x", 2100)}, ErrRejectedURL},
		{"empty", AddRequest{Name: "p8", URL: ""}, ErrRejectedURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := env.mgr.Add(context.Background(), tt.req, "huy")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add error %v, want %v", err, tt.wantErr)
			}
			if _, ok := env.reg.Kind(tt.req.Name); ok {
				t.Fatal("rejected source was registered")
			}
			if _, err := os.Stat(env.sourceFile(tt.req.Name)); err == nil {
				t.Fatal("rejected source was saved")
			}
		})
	}
	if env.srv.hits.Load() != hits {
		t.Fatal("a validation failure before the fetch still fetched the feed")
	}
}

func TestAddFetchFailureSavesNothing(t *testing.T) {
	cases := map[string]struct {
		url     string
		handler http.HandlerFunc
	}{
		"server error":   {"https://example.com:%s/feed/" + mgrToken + "/cal.ics", statusCode(http.StatusInternalServerError)},
		"malformed body": {"https://example.com:%s/feed/" + mgrToken + "/cal.ics", serveFeed([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n"))},
		"private name":   {"https://private.example:%s/feed/" + mgrToken + "/cal.ics", nil},
		"lan name":       {"https://lan.example:%s/feed/" + mgrToken + "/cal.ics", nil},
		"redirect to private": {"https://example.com:%s/redirect/" + mgrToken, func(w http.ResponseWriter, r *http.Request) {
			_, port, _ := net.SplitHostPort(r.Host)
			http.Redirect(w, r, "https://private.example:"+port+"/feed/"+mgrToken+"/cal.ics", http.StatusFound)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
			if tc.handler != nil {
				env.srv.setMode(tc.handler)
			}
			address := fmt.Sprintf(tc.url, env.port)
			_, err := env.mgr.Add(context.Background(), AddRequest{Name: "probe", URL: address}, "huy")
			if !errors.Is(err, ErrRejectedURL) {
				t.Fatalf("Add error %v, want ErrRejectedURL", err)
			}
			if _, ok := env.reg.Kind("probe"); ok {
				t.Fatal("failed validation left a registered source")
			}
			if _, err := os.Stat(env.sourceFile("probe")); err == nil {
				t.Fatal("failed validation saved the source file")
			}
			if _, err := os.Stat(env.snapshotFile("probe")); err == nil {
				t.Fatal("failed validation saved a snapshot")
			}
		})
	}
}

func TestAddDuplicateAndConfigNamesAreErrExists(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	address := env.addr("/feed/" + mgrToken + "/cal.ics")
	env.add("personal", address)
	for _, name := range []string{"personal", "team", "xnys"} {
		if _, err := env.mgr.Add(context.Background(), AddRequest{Name: name, URL: address}, "huy"); !errors.Is(err, ErrExists) {
			t.Fatalf("Add(%s) error %v, want ErrExists", name, err)
		}
	}
	if origin, _ := env.reg.Origin("team"); origin != OriginConfig {
		t.Fatal("config source changed by a refused add")
	}
}

func TestAddLimitIsErrLimit(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{MaxSources: 2})
	address := env.addr("/feed/" + mgrToken + "/cal.ics")
	env.add("one", address)
	env.add("two", address)
	if _, err := env.mgr.Add(context.Background(), AddRequest{Name: "three", URL: address}, "huy"); !errors.Is(err, ErrLimit) {
		t.Fatalf("third add error %v, want ErrLimit", err)
	}
	if _, ok := env.reg.Kind("three"); ok {
		t.Fatal("over-limit source was registered")
	}
}

func TestDefaultLimitIs32RuntimeSources(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	address := env.addr("/feed/" + mgrToken + "/cal.ics")
	for i := 0; i < 32; i++ {
		env.add(fmt.Sprintf("s%02d", i), address)
	}
	if _, err := env.mgr.Add(context.Background(), AddRequest{Name: "s32", URL: address}, "huy"); !errors.Is(err, ErrLimit) {
		t.Fatalf("33rd source error %v, want ErrLimit", err)
	}
}

func TestConcurrentAddsSerializeAndAllPersist(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	address := env.addr("/feed/" + mgrToken + "/cal.ics")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := env.mgr.Add(context.Background(), AddRequest{Name: fmt.Sprintf("c%d", i), URL: address}, "huy")
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent add: %v", err)
		}
	}
	loaded, issues := newSourceStore(env.dataDir).loadAll()
	if len(loaded) != 8 || len(issues) != 0 {
		t.Fatalf("persisted %d sources (issues %v), want 8", len(loaded), issues)
	}
}

func TestUpdatePollChangesIntervalWithoutFetching(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	hits := env.srv.hits.Load()
	poll := 10 * time.Minute
	st, err := env.mgr.Update(context.Background(), "personal", UpdateRequest{Poll: &poll}, "huy")
	if err != nil {
		t.Fatalf("Update poll: %v", err)
	}
	if st.Poll != 10*time.Minute {
		t.Fatalf("poll after update: %v", st.Poll)
	}
	if env.srv.hits.Load() != hits {
		t.Fatal("a poll-only update fetched the feed")
	}
	loaded, _ := newSourceStore(env.dataDir).loadAll()
	if len(loaded) != 1 || loaded[0].Poll != 10*time.Minute || !strings.Contains(loaded[0].URL, mgrToken) {
		t.Fatalf("persisted record after poll update: %+v", loaded)
	}
}

func TestUpdateAddressReplacesFeedAndSnapshot(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/OLD-TOKEN/cal.ics"))
	before, _, _ := env.reg.Occurrences("personal")
	if len(before) != 1 || before[0].Title != "Standup" {
		t.Fatalf("setup occurrences: %+v", before)
	}

	env.srv.setMode(serveFeed(icsFeed(icsEvent{uid: "new", title: "Replacement", start: env.clock.Now().Add(2 * time.Hour)})))
	newAddr := env.addr("/feed/NEW-TOKEN/cal.ics")
	if _, err := env.mgr.Update(context.Background(), "personal", UpdateRequest{URL: &newAddr}, "huy"); err != nil {
		t.Fatalf("Update url: %v", err)
	}
	after, _, _ := env.reg.Occurrences("personal")
	if len(after) != 1 || after[0].Title != "Replacement" {
		t.Fatalf("old feed's events survived the address change: %+v", after)
	}
	loaded, _ := newSourceStore(env.dataDir).loadAll()
	if len(loaded) != 1 || loaded[0].URL != newAddr {
		t.Fatalf("persisted address not replaced: %+v", loaded)
	}
	env.poller.Poll(context.Background(), "personal")
	if got := env.srv.lastPath(); got != "/feed/NEW-TOKEN/cal.ics" {
		t.Fatalf("next poll used %q", got)
	}
}

func TestUpdateURLValidatesBeforeChangingAnything(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	before, err := os.ReadFile(env.sourceFile("personal"))
	if err != nil {
		t.Fatal(err)
	}
	statusBefore := statusNamed(t, env.reg, "personal")
	env.srv.setMode(statusCode(http.StatusServiceUnavailable))

	bad := []string{
		"http://example.com/feed/" + mgrToken,
		"https://private.example:" + env.port + "/feed/" + mgrToken,
		env.addr("/feed/SERVER-DOWN/cal.ics"),
	}
	for _, u := range bad {
		u := u
		if _, err := env.mgr.Update(context.Background(), "personal", UpdateRequest{URL: &u}, "huy"); !errors.Is(err, ErrRejectedURL) {
			t.Fatalf("Update(%q) error %v, want ErrRejectedURL", u, err)
		}
	}
	after, err := os.ReadFile(env.sourceFile("personal"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a rejected address changed the persisted record")
	}
	if got := statusNamed(t, env.reg, "personal"); got.Poll != statusBefore.Poll || got.EventCount != statusBefore.EventCount {
		t.Fatalf("a rejected address changed the status: %+v", got)
	}
}

func TestUpdateErrors(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	u := env.addr("/feed/OTHER/cal.ics")
	zero := time.Duration(0)
	tiny := 10 * time.Second
	tests := []struct {
		name    string
		target  string
		req     UpdateRequest
		wantErr error
	}{
		{"unknown name", "nope", UpdateRequest{URL: &u}, ErrNotFound},
		{"config ics source", "team", UpdateRequest{URL: &u}, ErrConfigSource},
		{"config builtin source", "xnys", UpdateRequest{Poll: &zero}, ErrConfigSource},
		{"nothing to change", "personal", UpdateRequest{}, ErrNothingToUpdate},
		{"poll too short", "personal", UpdateRequest{Poll: &tiny}, ErrInvalidPoll},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := env.mgr.Update(context.Background(), tt.target, tt.req, "huy")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Update error %v, want %v", err, tt.wantErr)
			}
		})
	}
	if st := statusNamed(t, env.reg, "personal"); st.Poll != 5*time.Minute {
		t.Fatalf("failed updates changed the poll to %v", st.Poll)
	}
}

func TestRemoveStopsPollingAndDeletesFiles(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { env.poller.Run(ctx); close(done) }()

	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	res, err := env.mgr.Remove(ctx, "personal", false, "huy")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.References) != 0 {
		t.Fatalf("unreferenced source reported references: %+v", res.References)
	}
	if _, ok := env.reg.Kind("personal"); ok {
		t.Fatal("removed source still registered")
	}
	for _, f := range []string{env.sourceFile("personal"), env.snapshotFile("personal")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("file survived removal: %s (%v)", f, err)
		}
	}
	hits := env.srv.hits.Load()
	time.Sleep(150 * time.Millisecond)
	if env.srv.hits.Load() != hits {
		t.Fatal("removed source kept polling")
	}
	cancel()
	<-done
}

func TestRemoveReferencedNeedsForce(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	refs := []Reference{{ID: "a1", Path: "automations/morning.md", Title: "Morning", Field: "trigger.calendar"}}
	env.refs.set("personal", refs...)

	res, err := env.mgr.Remove(context.Background(), "personal", false, "huy")
	var refErr *ReferencedError
	if !errors.As(err, &refErr) {
		t.Fatalf("Remove without force: %v, want *ReferencedError", err)
	}
	if len(refErr.References) != 1 || refErr.References[0].Field != "trigger.calendar" {
		t.Fatalf("ReferencedError references: %+v", refErr.References)
	}
	if len(res.References) != 1 {
		t.Fatalf("RemoveResult references: %+v", res.References)
	}
	if _, ok := env.reg.Kind("personal"); !ok {
		t.Fatal("a refused removal unregistered the source")
	}
	if _, err := os.Stat(env.sourceFile("personal")); err != nil {
		t.Fatalf("a refused removal deleted the source file: %v", err)
	}

	res, err = env.mgr.Remove(context.Background(), "personal", true, "huy")
	if err != nil {
		t.Fatalf("forced Remove: %v", err)
	}
	if len(res.References) != 1 || res.References[0].ID != "a1" {
		t.Fatalf("forced removal did not report references: %+v", res.References)
	}
	if _, ok := env.reg.Kind("personal"); ok {
		t.Fatal("forced removal left the source registered")
	}
}

func TestRemoveFailsClosedWhenReferencesCannotBeChecked(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	env.refs.err = errors.New("automation store unavailable")
	if _, err := env.mgr.Remove(context.Background(), "personal", true, "huy"); err == nil {
		t.Fatal("Remove proceeded without knowing its references")
	}
	if _, ok := env.reg.Kind("personal"); !ok {
		t.Fatal("removal went ahead when the reference check failed")
	}
}

func TestRemoveConfigAndUnknownSources(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	if _, err := env.mgr.Remove(context.Background(), "team", true, "huy"); !errors.Is(err, ErrConfigSource) {
		t.Fatalf("remove config ics: %v want ErrConfigSource", err)
	}
	if _, err := env.mgr.Remove(context.Background(), "xnys", true, "huy"); !errors.Is(err, ErrConfigSource) {
		t.Fatalf("remove config builtin: %v want ErrConfigSource", err)
	}
	if _, err := env.mgr.Remove(context.Background(), "nope", true, "huy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown: %v want ErrNotFound", err)
	}
	if _, ok := env.reg.Kind("team"); !ok {
		t.Fatal("config source removed")
	}
}

func TestWritesAreLoggedWithNameAndActorNeverAddress(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	env.add("personal", env.addr("/feed/"+mgrToken+"/cal.ics"))
	poll := 15 * time.Minute
	if _, err := env.mgr.Update(context.Background(), "personal", UpdateRequest{Poll: &poll}, "dana"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.mgr.Remove(context.Background(), "personal", false, "dana"); err != nil {
		t.Fatal(err)
	}
	logs := env.logs.String()
	for _, want := range []string{"calendar source added", "calendar source updated", "calendar source removed", "personal", "huy", "dana"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log lacks %q:\n%s", want, logs)
		}
	}
}

// TestSecretTokenNeverLeaks drives every write path that can see the address
// and checks the token is absent from every error, log line, status, source
// listing, snapshot file and reload issue. Only the source's own record holds
// the address, because it must, and it is excluded from the check.
func TestSecretTokenNeverLeaks(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	ctx := context.Background()
	var texts []string
	record := func(err error) {
		if err != nil {
			texts = append(texts, err.Error())
		}
	}
	good := env.addr("/feed/" + mgrToken + "/cal.ics")

	_, err := env.mgr.Add(ctx, AddRequest{Name: "ok", URL: good}, "huy")
	record(err)
	_, err = env.mgr.Add(ctx, AddRequest{Name: "plain", URL: "http://example.com/feed/" + mgrToken}, "huy")
	record(err)
	_, err = env.mgr.Add(ctx, AddRequest{Name: "garbage", URL: "https://exa mple.com/feed/" + mgrToken}, "huy")
	record(err)
	_, err = env.mgr.Add(ctx, AddRequest{Name: "userinfo", URL: "https://u:" + mgrToken + "@example.com:" + env.port + "/x"}, "huy")
	record(err)
	_, err = env.mgr.Add(ctx, AddRequest{Name: "private", URL: "https://private.example:" + env.port + "/feed/" + mgrToken}, "huy")
	record(err)

	env.srv.setMode(statusCode(http.StatusInternalServerError))
	_, err = env.mgr.Add(ctx, AddRequest{Name: "down", URL: env.addr("/feed/" + mgrToken + "/cal.ics")}, "huy")
	record(err)
	env.srv.setMode(serveFeed(icsFeed(icsEvent{uid: "std", title: "Standup", start: env.clock.Now().Add(time.Hour)})))

	_, err = env.mgr.Update(ctx, "ok", UpdateRequest{URL: ptrString("https://private.example:" + env.port + "/feed/" + mgrToken)}, "huy")
	record(err)
	poll := 20 * time.Minute
	_, err = env.mgr.Update(ctx, "ok", UpdateRequest{Poll: &poll}, "huy")
	record(err)

	env.refs.set("ok", Reference{ID: "r", Path: "a.md", Title: "t", Field: "trigger.calendar"})
	res, err := env.mgr.Remove(ctx, "ok", false, "huy")
	record(err)
	texts = append(texts, fmt.Sprintf("%+v", res))

	// A corrupt source file holding the token must produce an issue, not a
	// leak. Its name is the only thing that may appear.
	bad := filepath.Join(env.dataDir, "calendars", "sources", "bad.json")
	if err := os.WriteFile(bad, []byte(`{"name":"bad","url":"`+mgrToken+`"`), 0o600); err != nil {
		t.Fatal(err)
	}
	env.open(mgrConfigs(), ManagerOptions{References: env.refs.find})

	for _, st := range env.reg.Statuses() {
		texts = append(texts, fmt.Sprintf("%+v", st))
	}
	texts = append(texts, env.logs.String())
	entries, err := os.ReadDir(filepath.Join(env.dataDir, "calendars"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(env.dataDir, "calendars", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, string(data))
	}
	for _, text := range texts {
		assertNoSecret(t, mgrToken, text)
	}
}

func ptrString(s string) *string { return &s }

// TestUpdateWhileRunPollsUsesTheNewAddress covers the write path the other
// run-time tests skip: an address change and a poll change applied while Run
// has live poll loops. The loops wait a full interval, so the new address is
// checked with a direct poll, which races the loops under -race.
func TestUpdateWhileRunPollsUsesTheNewAddress(t *testing.T) {
	env := newMgrEnv(t, mgrConfigs(), ManagerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { env.poller.Run(ctx); close(done) }()

	env.add("personal", env.addr("/feed/OLD-TOKEN/cal.ics"))
	newAddr := env.addr("/feed/NEW-TOKEN/cal.ics")
	if _, err := env.mgr.Update(ctx, "personal", UpdateRequest{URL: &newAddr}, "huy"); err != nil {
		t.Fatalf("Update url while running: %v", err)
	}
	env.poller.Poll(ctx, "personal")
	if got := env.srv.lastPath(); got != "/feed/NEW-TOKEN/cal.ics" {
		t.Fatalf("poll after address change used %q", got)
	}
	poll := 2 * time.Minute
	if st, err := env.mgr.Update(ctx, "personal", UpdateRequest{Poll: &poll}, "huy"); err != nil || st.Poll != 2*time.Minute {
		t.Fatalf("Update poll while running: st=%+v err=%v", st, err)
	}
	if _, err := env.mgr.Remove(ctx, "personal", false, "huy"); err != nil {
		t.Fatalf("Remove while running: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
