package mcp

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestZZProbeWildcards(t *testing.T) {
	for _, f := range []string{"*", ".*", "goal.reconcile", "goal.*", "task.*", "task", "runner.*", "webhook.*", "project.*", "entry.*", "control.*", "feature.*"} {
		err := validateEventTypeFilter(f)
		// also what the server would do with it
		serverMatchesSomething := false
		for _, k := range types.AllEventTypes {
			if types.MatchEventPattern(f, k) {
				serverMatchesSomething = true
				break
			}
		}
		t.Logf("filter=%-16q validateErr=%v serverCouldMatchKnownType=%v", f, err != nil, serverMatchesSomething)
	}
}
