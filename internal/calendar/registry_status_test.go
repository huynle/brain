package calendar

import (
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

func statusRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(map[string]config.CalendarConfig{
		"xnys": {Type: "builtin", Market: "XNYS"},
		"team": {Type: "ics", URLEnv: "TEAM_CAL_URL"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestStatusesCoverEverySourceSortedWithBuiltinShape(t *testing.T) {
	statuses := statusRegistry(t).Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses: got %d want 2: %+v", len(statuses), statuses)
	}
	if statuses[0].Name != "team" || statuses[1].Name != "xnys" {
		t.Fatalf("Statuses not sorted by name: %+v", statuses)
	}
	builtin := statuses[1]
	if builtin.Kind != KindBuiltin {
		t.Fatalf("builtin kind: got %q", builtin.Kind)
	}
	if !builtin.LastFetch.IsZero() || !builtin.LastSuccess.IsZero() || builtin.EventCount != 0 || builtin.LastError != "" || builtin.Stale {
		t.Fatalf("builtin carries fetch fields: %+v", builtin)
	}
}

func TestOccurrencesUnknownNameIsAbsent(t *testing.T) {
	occ, status, ok := statusRegistry(t).Occurrences("nope")
	if ok || occ != nil || status.Name != "" {
		t.Fatalf("unknown name: occ=%v status=%+v ok=%v", occ, status, ok)
	}
}

func TestOccurrencesBuiltinSourceHasNoEvents(t *testing.T) {
	occ, status, ok := statusRegistry(t).Occurrences("xnys")
	if !ok {
		t.Fatal("builtin source reported as unknown")
	}
	if len(occ) != 0 {
		t.Fatalf("builtin occurrences: %v", occ)
	}
	if status.Name != "xnys" || status.Kind != KindBuiltin {
		t.Fatalf("builtin status: %+v", status)
	}
}

func TestPublishedSnapshotIsServedAsCopy(t *testing.T) {
	r := statusRegistry(t)
	fetched := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r.setSnapshot(snapshot{
		Name:        "team",
		LastFetch:   fetched,
		LastSuccess: fetched,
		LastError:   "HTTP 503",
		Stale:       true,
		Occurrences: []Occurrence{
			{UID: "a", Calendar: "team", Title: "one"},
			{UID: "b", Calendar: "team", Title: "two"},
		},
	})

	occ, status, ok := r.Occurrences("team")
	if !ok || len(occ) != 2 {
		t.Fatalf("Occurrences: ok=%v len=%d", ok, len(occ))
	}
	if status.Kind != KindICS || status.EventCount != 2 || status.LastError != "HTTP 503" || !status.Stale {
		t.Fatalf("ics status: %+v", status)
	}
	if !status.LastSuccess.Equal(fetched) || !status.LastFetch.Equal(fetched) {
		t.Fatalf("ics times: %+v", status)
	}

	occ[0].Title = "mutated by caller"
	again, _, _ := r.Occurrences("team")
	if again[0].Title != "one" {
		t.Fatal("caller mutation reached the registry: Occurrences must return a copy")
	}

	var found bool
	for _, s := range r.Statuses() {
		if s.Name == "team" {
			found = true
			if s.EventCount != 2 || !s.Stale {
				t.Fatalf("Statuses entry for team: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("team missing from Statuses")
	}
}

func TestNilRegistryKnowsNothing(t *testing.T) {
	var r *Registry
	if got := r.Statuses(); len(got) != 0 {
		t.Fatalf("nil Statuses: %v", got)
	}
	if _, _, ok := r.Occurrences("team"); ok {
		t.Fatal("nil registry reported a source")
	}
}
