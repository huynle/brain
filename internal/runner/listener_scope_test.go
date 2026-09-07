package runner

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// A real, separately owned process named opencode, discoverable by lsof.
func TestListenerScopeProcess(t *testing.T) {
	if os.Getenv("BRAIN_LISTENER_SCOPE_HELPER") != "1" {
		return
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%d %d\n", os.Getpid(), l.Addr().(*net.TCPAddr).Port)
	_ = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(os.Getenv("BRAIN_LISTENER_SCOPE_HITS"), []byte(r.URL.Path), 0o600); err != nil {
			panic(err)
		}
		_, _ = w.Write([]byte(`[{"id":"real-message"}]`))
	}))
	os.Exit(0)
}

func TestListenerScope_RealDiscovery(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "opencode")
	if err := os.WriteFile(helper, bin, 0o700); err != nil {
		t.Fatal(err)
	}
	start := func(name string, shell bool) (Process, OpencodeListener, string) {
		t.Helper()
		hits := filepath.Join(dir, name+"-hits")
		cmd := exec.Command(helper, "-test.run=^TestListenerScopeProcess$")
		if shell {
			cmd = exec.Command("/bin/sh", "-c", `"$1" -test.run='^TestListenerScopeProcess$' & wait`, "sh", helper)
		}
		cmd.Env = append(os.Environ(), "BRAIN_LISTENER_SCOPE_HELPER=1", "BRAIN_LISTENER_SCOPE_HITS="+hits)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		proc := NewOsProcess(cmd) // sole Cmd.Wait owner
		var child int
		t.Cleanup(func() {
			if child > 0 {
				p, _ := os.FindProcess(child)
				_ = p.Kill()
			}
			_ = proc.Kill(os.Kill)
			select {
			case <-proc.Done():
			case <-time.After(5 * time.Second):
				t.Error("helper did not exit")
			}
		})
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("helper output: %q", line)
		}
		child, err = strconv.Atoi(fields[0])
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatal(err)
		}
		return proc, OpencodeListener{PID: child, Port: port}, hits
	}
	_, unrelated, unrelatedHits := start("unrelated", false)
	driver, owned, ownedHits := start("owned", true)
	if driver.Pid() == owned.PID {
		t.Fatal("fixture needs a shell parent and OpenCode descendant")
	}
	pm := NewProcessManager(RunnerConfig{})
	if err := pm.Add("task", RunningTask{ID: "task", ExecutorType: "opencode"}, driver); err != nil {
		t.Fatal(err)
	}
	bc := NewBridgeClient(&TaskRunner{processMgr: pm})
	// Prove lsof sees both, without sending either an HTTP request.
	allFixture := DiscoverOpencodeListeners(map[int]bool{unrelated.PID: true, owned.PID: true})
	if len(allFixture) != 2 {
		t.Fatalf("lsof fixture listeners = %v, want both", allFixture)
	}
	if got := DiscoverOpencodeListeners(nil); len(got) != 0 {
		t.Fatalf("unscoped discovery returned %v", got)
	}
	if got := bc.externalListeners(); len(got) != 1 || got[0] != owned {
		t.Fatalf("scoped discovery = %v, want %v", got, owned)
	}
	if got := bc.portForExternalSession("ses_real"); got != owned.Port {
		t.Fatalf("selected %d, want owned %d", got, owned.Port)
	}
	if data, err := os.ReadFile(ownedHits); err != nil || string(data) != "/session/ses_real/message" {
		t.Fatalf("owned endpoint evidence: %q %v", data, err)
	}
	if _, err := os.Stat(unrelatedHits); !os.IsNotExist(err) {
		t.Fatalf("unrelated listener was probed: %v", err)
	}
	pm.Remove("task")
	if got := bc.portForExternalSession("ses_real"); got != 0 {
		t.Fatalf("revoked live process selected: %d", got)
	}
}

func scopeListener(t *testing.T) (int, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`[{"id":"message"}]`))
	}))
	t.Cleanup(s.Close)
	return s.Listener.Addr().(*net.TCPAddr).Port, hits
}

func cacheScopeListeners(bc *BridgeClient, listeners ...OpencodeListener) {
	bc.externalListenersCached = listeners
	bc.externalListenersAt = time.Now()
}

func TestListenerScope_UnrelatedLocalhostNeverProbed(t *testing.T) {
	port, hits := scopeListener(t)
	bc := NewBridgeClient(&TaskRunner{processMgr: NewProcessManager(RunnerConfig{})})
	cacheScopeListeners(bc, OpencodeListener{PID: os.Getpid(), Port: port})
	if got := bc.portForExternalSession("ses_unrelated"); got != 0 {
		t.Errorf("selected unrelated localhost listener: %d", got)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("unrelated listener received %d HTTP probes, want zero", got)
	}
}

func TestListenerScope_OwnershipAndRevocation(t *testing.T) {
	old := sampleProcessTable
	t.Cleanup(func() { sampleProcessTable = old })
	table := map[int]procSample{10: {PID: 10}, 11: {PID: 11, PPID: 10}, 20: {PID: 20}, 21: {PID: 21, PPID: 20}, 30: {PID: 30}, 31: {PID: 31, PPID: 30}, 99: {PID: 99}}
	sampleProcessTable = func() (map[int]procSample, error) { return table, nil }
	for _, tc := range []struct {
		name string
		pid  int
		kind string
	}{
		{"driver", 10, "task"}, {"tmux descendant", 11, "task"},
		{"serve after driver exits", 20, "serve"}, {"serve descendant", 21, "serve"},
		{"adhoc", 30, "adhoc"}, {"adhoc descendant", 31, "adhoc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ownedPort, ownedHits := scopeListener(t)
			otherPort, otherHits := scopeListener(t)
			pm := NewProcessManager(RunnerConfig{})
			e := &serveAwareExecutor{mockExecutor: newMockExecutor(), servePIDs: map[string]int{}}
			bc := NewBridgeClient(&TaskRunner{processMgr: pm, executor: e})
			if tc.kind == "adhoc" {
				bc.adhoc["ad"] = &adhocInstance{proc: newMockProcess(30), Instance: types.OpencodeInstance{Executor: "opencode"}}
			} else {
				p := newMockProcess(10)
				if tc.kind == "serve" {
					p.exited = true
					e.servePIDs["task"] = 20
				}
				if err := pm.Add("task", RunningTask{ID: "task", ExecutorType: "opencode"}, p); err != nil {
					t.Fatal(err)
				}
			}
			cacheScopeListeners(bc, OpencodeListener{PID: 99, Port: otherPort}, OpencodeListener{PID: tc.pid, Port: ownedPort})
			if got := bc.portForExternalSession("ses_target"); got != ownedPort {
				t.Errorf("port = %d, want owned %d", got, ownedPort)
			}
			if otherHits.Load() != 0 {
				t.Error("unowned mixed candidate was probed")
			}
			if ownedHits.Load() != 1 {
				t.Error("owned candidate was not probed once")
			}
			pm.Remove("task")
			delete(bc.adhoc, "ad")
			if got := bc.portForExternalSession("ses_target"); got != 0 {
				t.Errorf("revoked cached ownership selected %d", got)
			}
			if ownedHits.Load() != 1 || otherHits.Load() != 0 {
				t.Error("revoked cache caused HTTP probe")
			}
		})
	}
}

func TestListenerScope_FailsClosed(t *testing.T) {
	old := sampleProcessTable
	t.Cleanup(func() { sampleProcessTable = old })
	for _, mode := range []string{"missing runner", "missing manager", "inspection error", "missing pid", "reservation", "pi", "exited", "adhoc metadata only"} {
		t.Run(mode, func(t *testing.T) {
			port, hits := scopeListener(t)
			pm := NewProcessManager(RunnerConfig{})
			bc := NewBridgeClient(&TaskRunner{processMgr: pm})
			sampleProcessTable = func() (map[int]procSample, error) { return map[int]procSample{10: {PID: 10}}, nil }
			p := newMockProcess(10)
			task := RunningTask{ID: "task", ExecutorType: "opencode"}
			switch mode {
			case "missing runner":
				bc.runner = nil
			case "missing manager":
				bc.runner.processMgr = nil
			case "inspection error":
				sampleProcessTable = func() (map[int]procSample, error) { return nil, errors.New("ps denied") }
			case "missing pid":
				sampleProcessTable = func() (map[int]procSample, error) { return nil, nil }
			case "pi":
				task.ExecutorType = "pi"
			case "exited":
				p.exited = true
			case "adhoc metadata only":
				bc.adhoc["ad"] = &adhocInstance{Instance: types.OpencodeInstance{PID: 10, Executor: "opencode"}}
			}
			if mode == "reservation" {
				pm.ReserveSlot("task", 1)
			} else if mode != "adhoc metadata only" {
				if err := pm.Add("task", task, p); err != nil {
					t.Fatal(err)
				}
			}
			cacheScopeListeners(bc, OpencodeListener{PID: 10, Port: port})
			if got := bc.portForExternalSession("ses_target"); got != 0 {
				t.Errorf("%s selected %d", mode, got)
			}
			if hits.Load() != 0 {
				t.Errorf("%s probed listener", mode)
			}
		})
	}
}
