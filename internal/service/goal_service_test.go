package service

import (
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// task is a tiny helper to build a ResolvedTask literal for table tests.
func task(id, title, status string) types.ResolvedTask {
	return types.ResolvedTask{ID: id, Title: title, Status: status}
}

func TestDecideReconcile(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.GoalConfig
		// tasks are the goal's linked tasks.
		tasks []types.ResolvedTask
		// want is the expected decision.
		want ReconcileDecision
	}{
		{
			name:  "empty tasks -> need_work",
			cfg:   types.GoalConfig{ID: "g1"},
			tasks: nil,
			want:  ReconcileNeedWork,
		},
		{
			name: "all completed -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "completed"),
			},
			want: ReconcileComplete,
		},
		{
			name: "mix of completed+validated all in complete set -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "validated"),
				task("t3", "C", "completed"),
			},
			want: ReconcileComplete,
		},
		{
			name: "one in_progress others pending/completed -> noop",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "in_progress"),
				task("t3", "C", "pending"),
			},
			want: ReconcileNoop,
		},
		{
			name: "one blocked none in_progress -> block",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "blocked"),
				task("t3", "C", "pending"),
			},
			want: ReconcileBlock,
		},
		{
			name: "all pending none active -> need_work",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "pending"),
				task("t2", "B", "pending"),
			},
			want: ReconcileNeedWork,
		},
		{
			name: "custom complete set only validated; completed task does not count -> not complete",
			cfg:  types.GoalConfig{ID: "g1", CompleteStatuses: []string{"validated"}},
			tasks: []types.ResolvedTask{
				task("t1", "A", "validated"),
				task("t2", "B", "completed"),
			},
			// "completed" is not in the custom complete set, and is not
			// in_progress/blocked/pending -> falls through to need_work.
			want: ReconcileNeedWork,
		},
		{
			name: "blocked + in_progress together -> noop (precedence)",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "in_progress"),
				task("t2", "B", "blocked"),
			},
			want: ReconcileNoop,
		},
		{
			name: "custom blocked set; cancelled counts as blocked -> block",
			cfg:  types.GoalConfig{ID: "g1", BlockedStatuses: []string{"blocked", "cancelled"}},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "cancelled"),
			},
			want: ReconcileBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := decideReconcile(tt.cfg, tt.tasks)
			if got != tt.want {
				t.Errorf("decideReconcile() decision = %q, want %q (reason: %q)", got, tt.want, reason)
			}
			if reason == "" {
				t.Errorf("decideReconcile() returned empty reason for decision %q", got)
			}
		})
	}
}

func TestLinkedTaskSnapshot(t *testing.T) {
	tasks := []types.ResolvedTask{
		task("t1", "First", "completed"),
		task("t2", "Second", "blocked"),
	}

	got := linkedTaskSnapshot(tasks)
	want := []LinkedTaskSnapshot{
		{ID: "t1", Title: "First", Status: "completed"},
		{ID: "t2", Title: "Second", Status: "blocked"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linkedTaskSnapshot() = %#v, want %#v", got, want)
	}
}

func TestLinkedTaskSnapshot_EmptyIsNonNil(t *testing.T) {
	got := linkedTaskSnapshot(nil)
	if got == nil {
		t.Fatal("linkedTaskSnapshot(nil) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("linkedTaskSnapshot(nil) len = %d, want 0", len(got))
	}

	got = linkedTaskSnapshot([]types.ResolvedTask{})
	if got == nil {
		t.Fatal("linkedTaskSnapshot([]) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("linkedTaskSnapshot([]) len = %d, want 0", len(got))
	}
}
