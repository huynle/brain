package schedule

import (
	"fmt"
	"testing"
	"time"
)

// Golden values: FNV-1a 64 of automationID + "\x00" + project, reduced
// modulo the window in whole seconds. Pinned so a change of hash, separator
// or granularity — any of which would move every project's run time — fails
// loudly.
func TestStaggerOffset_Golden(t *testing.T) {
	tests := []struct {
		id, project string
		window      time.Duration
		want        time.Duration
	}{
		// FNV-1a 64("brain:builtin-dream-consolidation\x00hindsight") = 2061881106749153363
		{"brain:builtin-dream-consolidation", "hindsight", 2 * time.Hour, 2963 * time.Second},
		// The separator keeps ("ab","c") and ("a","bc") apart.
		{"ab", "c", 2 * time.Hour, 5735 * time.Second},
		{"a", "bc", 2 * time.Hour, 1699 * time.Second},
		{"", "", 2 * time.Hour, 255 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.project, func(t *testing.T) {
			if got := StaggerOffset(tt.id, tt.project, tt.window); got != tt.want {
				t.Errorf("StaggerOffset(%q, %q, %v) = %v, want %v", tt.id, tt.project, tt.window, got, tt.want)
			}
		})
	}
}

func TestStaggerOffset_NoWindow(t *testing.T) {
	for _, w := range []time.Duration{0, -time.Hour, time.Nanosecond, 999 * time.Millisecond} {
		if got := StaggerOffset("auto", "proj", w); got != 0 {
			t.Errorf("StaggerOffset(window=%v) = %v, want 0", w, got)
		}
	}
}

func TestStaggerOffset_InRangeWholeSeconds(t *testing.T) {
	windows := []time.Duration{time.Second, 1500 * time.Millisecond, 90 * time.Second, 2 * time.Hour, 7 * 24 * time.Hour}
	for _, w := range windows {
		for i := 0; i < 500; i++ {
			got := StaggerOffset(fmt.Sprintf("auto-%d", i%7), fmt.Sprintf("project-%d", i), w)
			if got < 0 || got >= w {
				t.Fatalf("window %v: offset %v out of [0, window)", w, got)
			}
			if got%time.Second != 0 {
				t.Fatalf("window %v: offset %v is not whole seconds", w, got)
			}
		}
	}
}

// The offset is a pure function of (automation, project): adding, removing
// or reordering other projects never moves an existing project's slot.
func TestStaggerOffset_StableAndIndependent(t *testing.T) {
	const id, window = "dream", 2 * time.Hour
	projects := []string{"alpha", "beta", "gamma", "delta"}
	before := map[string]time.Duration{}
	for _, p := range projects {
		before[p] = StaggerOffset(id, p, window)
	}
	// Evaluate again in reverse order, interleaved with unrelated projects.
	for i := len(projects) - 1; i >= 0; i-- {
		_ = StaggerOffset(id, fmt.Sprintf("new-%d", i), window)
		p := projects[i]
		if got := StaggerOffset(id, p, window); got != before[p] {
			t.Errorf("offset for %q moved from %v to %v", p, before[p], got)
		}
	}
}

// Offsets spread across the window rather than clustering.
func TestStaggerOffset_Spreads(t *testing.T) {
	const window = 2 * time.Hour
	seen := map[time.Duration]bool{}
	var buckets [4]int
	for i := 0; i < 200; i++ {
		off := StaggerOffset("dream", fmt.Sprintf("project-%d", i), window)
		seen[off] = true
		buckets[off*4/window]++
	}
	if len(seen) < 190 {
		t.Errorf("only %d distinct offsets for 200 projects", len(seen))
	}
	for q, n := range buckets {
		if n < 25 {
			t.Errorf("quarter %d of the window holds only %d of 200 offsets: %v", q, n, buckets)
		}
	}
}
