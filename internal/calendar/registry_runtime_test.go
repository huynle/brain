package calendar

import (
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

func runtimeTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(map[string]config.CalendarConfig{
		"xnys": {Type: KindBuiltin, Market: "XNYS"},
		"team": {Type: KindICS, URLEnv: "TEAM_CAL_URL", Poll: "2m"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func statusNamed(t *testing.T, r *Registry, name string) Status {
	t.Helper()
	for _, s := range r.Statuses() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("%s missing from Statuses", name)
	return Status{}
}

func TestConfigSourcesReportOriginAndPoll(t *testing.T) {
	r := runtimeTestRegistry(t)
	team := statusNamed(t, r, "team")
	if team.Origin != OriginConfig || team.Poll != 2*time.Minute {
		t.Fatalf("config ics status: origin=%q poll=%v want config 2m", team.Origin, team.Poll)
	}
	xnys := statusNamed(t, r, "xnys")
	if xnys.Origin != OriginConfig || xnys.Poll != 0 {
		t.Fatalf("builtin status: origin=%q poll=%v want config 0", xnys.Origin, xnys.Poll)
	}
}

func TestConfigICSWithoutPollReportsDefaultFiveMinutes(t *testing.T) {
	r, err := NewRegistry(map[string]config.CalendarConfig{"team": {Type: KindICS, URLEnv: "X"}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got := statusNamed(t, r, "team").Poll; got != 5*time.Minute {
		t.Fatalf("default poll: got %v want 5m", got)
	}
}

func TestOriginOfNamesAndNilRegistry(t *testing.T) {
	r := runtimeTestRegistry(t)
	if origin, ok := r.Origin("team"); !ok || origin != OriginConfig {
		t.Fatalf("Origin(team) = (%q, %v)", origin, ok)
	}
	if _, ok := r.Origin("missing"); ok {
		t.Fatal("Origin(missing) ok = true")
	}
	var nilReg *Registry
	if _, ok := nilReg.Origin("team"); ok {
		t.Fatal("nil registry knows an origin")
	}
}

func TestAddICSRegistersRuntimeSourceAndRemoveUnregisters(t *testing.T) {
	r := runtimeTestRegistry(t)
	if err := r.addICS("personal", OriginRuntime, 10*time.Minute); err != nil {
		t.Fatalf("addICS: %v", err)
	}
	if kind, ok := r.Kind("personal"); !ok || kind != KindICS {
		t.Fatalf("Kind(personal) = (%q, %v)", kind, ok)
	}
	s := statusNamed(t, r, "personal")
	if s.Origin != OriginRuntime || s.Poll != 10*time.Minute || s.Kind != KindICS {
		t.Fatalf("runtime status: %+v", s)
	}
	if _, _, ok := r.Window("personal"); ok {
		t.Fatal("never-fetched runtime source reports a window")
	}

	r.removeICS("personal")
	if _, ok := r.Kind("personal"); ok {
		t.Fatal("removed source still known")
	}
	for _, st := range r.Statuses() {
		if st.Name == "personal" {
			t.Fatal("removed source still in Statuses")
		}
	}
}

func TestAddICSRefusesAnyExistingName(t *testing.T) {
	r := runtimeTestRegistry(t)
	for _, name := range []string{"team", "xnys"} {
		if err := r.addICS(name, OriginRuntime, time.Minute); err == nil {
			t.Fatalf("addICS(%q) over an existing source = nil error", name)
		}
	}
	if origin, _ := r.Origin("team"); origin != OriginConfig {
		t.Fatalf("refused add changed the config source origin to %q", origin)
	}
}

func TestRemoveICSLeavesConfigSourcesAlone(t *testing.T) {
	r := runtimeTestRegistry(t)
	r.removeICS("team")
	r.removeICS("xnys")
	if _, ok := r.Kind("team"); !ok {
		t.Fatal("removeICS dropped a config ics source")
	}
	if _, ok := r.DayCalendar("xnys"); !ok {
		t.Fatal("removeICS dropped a config builtin source")
	}
}

func TestSetPollChangesReportedInterval(t *testing.T) {
	r := runtimeTestRegistry(t)
	if err := r.addICS("personal", OriginRuntime, time.Minute); err != nil {
		t.Fatalf("addICS: %v", err)
	}
	r.setPoll("personal", 15*time.Minute)
	if got := statusNamed(t, r, "personal").Poll; got != 15*time.Minute {
		t.Fatalf("poll after setPoll: %v", got)
	}
}

// TestRegistryReadsAreSafeDuringWrites drives every read method while sources
// are added and removed. Run with -race: a missing lock fails the detector.
func TestRegistryReadsAreSafeDuringWrites(t *testing.T) {
	r := runtimeTestRegistry(t)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = r.Statuses()
				_, _, _ = r.Occurrences("personal")
				_, _ = r.Kind("personal")
				_, _ = r.Origin("personal")
				_, _ = r.DayCalendar("xnys")
				_, _, _ = r.Window("personal")
			}
		}()
	}
	for i := 0; i < 200; i++ {
		if err := r.addICS("personal", OriginRuntime, time.Minute); err != nil {
			t.Fatalf("addICS round %d: %v", i, err)
		}
		r.setSnapshot(snapshot{Name: "personal", LastSuccess: time.Unix(int64(i), 0).UTC()})
		r.setPoll("personal", 2*time.Minute)
		r.removeICS("personal")
	}
	close(stop)
	readers.Wait()
}
