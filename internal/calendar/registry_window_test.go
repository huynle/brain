package calendar

import (
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

// Window reports the span a source's latest full (200) fetch expanded. The
// poller expands [poll − 24h, poll + 14d) and records the end, so the start
// is 15 days before the recorded end.
func TestRegistryWindowIsTheLastFullFetchSpan(t *testing.T) {
	reg, err := NewRegistry(map[string]config.CalendarConfig{
		"team": {Type: KindICS, URLEnv: "TEAM_CAL_URL"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	end := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	reg.setSnapshot(snapshot{Name: "team", WindowEnd: end})

	start, gotEnd, ok := reg.Window("team")
	if !ok {
		t.Fatal("Window(team) ok = false, want true")
	}
	if want := end.Add(-15 * 24 * time.Hour); !start.Equal(want) {
		t.Fatalf("Window start = %s, want %s", start, want)
	}
	if !gotEnd.Equal(end) {
		t.Fatalf("Window end = %s, want %s", gotEnd, end)
	}
}

// Without a full fetch there is no window: no snapshot, a snapshot whose
// fetches have only failed, an unknown name, a builtin day calendar and a nil
// registry all report ok false.
func TestRegistryWindowUnknownWithoutAFullFetch(t *testing.T) {
	reg, err := NewRegistry(map[string]config.CalendarConfig{
		"team": {Type: KindICS, URLEnv: "TEAM_CAL_URL"},
		"xnys": {Type: KindBuiltin, Market: "XNYS"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	reg.setSnapshot(snapshot{Name: "team", LastError: "fetch failed"})

	cases := map[string]*Registry{"registry": reg, "nil registry": nil}
	for label, r := range cases {
		for _, name := range []string{"team", "xnys", "missing"} {
			if _, _, ok := r.Window(name); ok {
				t.Errorf("%s: Window(%q) ok = true, want false", label, name)
			}
		}
	}
}
