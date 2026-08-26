package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// Coverage for the bounded-retry path. A failed/crashed/timed-out task is
// reset to "pending" so it can be retried; nothing counted those retries, so a
// task failing deterministically (a guard exiting non-zero, a missing input)
// was re-dispatched every poll interval forever, taking a runner slot each
// time. AutomationRetry.MaxAttempts existed but had no consumer.

func TestResolveMaxAttempts_Precedence(t *testing.T) {
	tests := []struct {
		name    string
		task    *types.ResolvedTask
		cfgMax  int
		want    int
		comment string
	}{
		{
			name:   "task retry wins over config",
			task:   &types.ResolvedTask{Retry: &types.AutomationRetry{MaxAttempts: 7}},
			cfgMax: 3,
			want:   7,
		},
		{
			name:   "config used when task has no retry block",
			task:   &types.ResolvedTask{},
			cfgMax: 5,
			want:   5,
		},
		{
			name:   "config used when task retry.max_attempts is unset",
			task:   &types.ResolvedTask{Retry: &types.AutomationRetry{Backoff: "fixed"}},
			cfgMax: 5,
			want:   5,
		},
		{
			name:   "built-in default when neither is set",
			task:   &types.ResolvedTask{},
			cfgMax: 0,
			want:   DefaultMaxTaskAttempts,
		},
		{
			name:    "negative config disables the cap",
			task:    &types.ResolvedTask{},
			cfgMax:  -1,
			want:    0,
			comment: "0 means uncapped to exhaustedAttempts",
		},
		{
			name:   "task retry still wins over a disabled config cap",
			task:   &types.ResolvedTask{Retry: &types.AutomationRetry{MaxAttempts: 2}},
			cfgMax: -1,
			want:   2,
		},
		{
			name: "nil task falls back to config",
			task: nil, cfgMax: 4, want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMaxAttempts(tt.task, RunnerConfig{MaxTaskAttempts: tt.cfgMax})
			if got != tt.want {
				t.Errorf("resolveMaxAttempts = %d, want %d (%s)", got, tt.want, tt.comment)
			}
		})
	}
}

func TestExhaustedAttempts(t *testing.T) {
	tests := []struct {
		attempt, max int
		want         bool
	}{
		{attempt: 1, max: 3, want: false},
		{attempt: 2, max: 3, want: false},
		{attempt: 3, max: 3, want: true},
		{attempt: 4, max: 3, want: true},
		{attempt: 1, max: 1, want: true}, // max_attempts=1 means never retry
		{attempt: 99, max: 0, want: false},
		{attempt: 99, max: -1, want: false},
	}
	for _, tt := range tests {
		if got := exhaustedAttempts(tt.attempt, tt.max); got != tt.want {
			t.Errorf("exhaustedAttempts(%d, %d) = %v, want %v", tt.attempt, tt.max, got, tt.want)
		}
	}
}

// newRetryTestRunner builds a runner wired only for the metadata/append calls
// the retry path makes.
func newRetryTestRunner(client *mockClient) *TaskRunner {
	return newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
}

func runningTaskForRetry(attemptCount, maxAttempts int) RunningTask {
	return RunningTask{
		ID:           "task-1",
		Path:         "projects/proj/task/task-1.md",
		ProjectID:    "proj",
		AttemptCount: attemptCount,
		MaxAttempts:  maxAttempts,
	}
}

func TestRecordTaskFailure_RetriesWhileUnderCap(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)

	status, attempt := tr.recordTaskFailure(context.Background(), runningTaskForRetry(0, 3))

	if status != "pending" {
		t.Errorf("status = %q, want \"pending\" (retries remain)", status)
	}
	if attempt != 1 {
		t.Errorf("attempt = %d, want 1", attempt)
	}

	writes := client.metadataWrites()
	if len(writes) != 1 {
		t.Fatalf("metadata writes = %d, want 1", len(writes))
	}
	if got := writes[0].Fields["attempt_count"]; got != 1 {
		t.Errorf("attempt_count written = %v, want 1", got)
	}
	if _, ok := writes[0].Fields["last_failed_at"]; !ok {
		t.Error("expected last_failed_at to be written alongside the counter")
	}
	if len(client.appendCalls) != 0 {
		t.Error("no cap note should be appended while retries remain")
	}
}

func TestRecordTaskFailure_BlocksAtCap(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)

	// Two prior failures, cap of 3: this failure is the last one allowed.
	status, attempt := tr.recordTaskFailure(context.Background(), runningTaskForRetry(2, 3))

	if status != "blocked" {
		t.Errorf("status = %q, want \"blocked\" (cap reached)", status)
	}
	if attempt != 3 {
		t.Errorf("attempt = %d, want 3", attempt)
	}
	if got := client.metadataWrites()[0].Fields["attempt_count"]; got != 3 {
		t.Errorf("attempt_count written = %v, want 3", got)
	}

	// "blocked" with no explanation is the same dead end as the silent
	// crash-loop, just quieter.
	if len(client.appendCalls) != 1 {
		t.Fatalf("append calls = %d, want 1 (cap note)", len(client.appendCalls))
	}
	note := client.appendCalls[0].Content
	if !strings.Contains(note, "Retry cap reached") {
		t.Errorf("cap note missing headline: %q", note)
	}
	if !strings.Contains(note, "3/3") {
		t.Errorf("cap note should state attempts used, got: %q", note)
	}
}

func TestRecordTaskFailure_UncappedNeverBlocks(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)

	status, attempt := tr.recordTaskFailure(context.Background(), runningTaskForRetry(99, 0))

	if status != "pending" {
		t.Errorf("status = %q, want \"pending\" (cap disabled)", status)
	}
	if attempt != 100 {
		t.Errorf("attempt = %d, want 100", attempt)
	}
}

func TestRecordTaskFailure_MetadataErrorStillReturnsStatus(t *testing.T) {
	// Losing the counter costs one extra retry; failing here would leave the
	// task stuck in_progress, which is strictly worse.
	client := newMockClient()
	client.metadataErr = context.DeadlineExceeded
	tr := newRetryTestRunner(client)

	status, attempt := tr.recordTaskFailure(context.Background(), runningTaskForRetry(2, 3))

	if status != "blocked" {
		t.Errorf("status = %q, want \"blocked\" even when the counter write failed", status)
	}
	if attempt != 3 {
		t.Errorf("attempt = %d, want 3", attempt)
	}
}

func TestClearTaskFailures_ResetsAfterSuccess(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)

	tr.clearTaskFailures(context.Background(), runningTaskForRetry(2, 3))

	writes := client.metadataWrites()
	if len(writes) != 1 {
		t.Fatalf("metadata writes = %d, want 1", len(writes))
	}
	if got := writes[0].Fields["attempt_count"]; got != 0 {
		t.Errorf("attempt_count written = %v, want 0", got)
	}
}

func TestClearTaskFailures_NoWriteWhenCounterAlreadyZero(t *testing.T) {
	// The overwhelmingly common case: every successful first-try task would
	// otherwise pay an extra API round-trip.
	client := newMockClient()
	tr := newRetryTestRunner(client)

	tr.clearTaskFailures(context.Background(), runningTaskForRetry(0, 3))

	if writes := client.metadataWrites(); len(writes) != 0 {
		t.Errorf("metadata writes = %d, want 0", len(writes))
	}
}

// TestRetryCapTerminatesLoop walks the exact live failure: a task that fails
// deterministically every run. Before the cap it returned "pending" forever.
func TestRetryCapTerminatesLoop(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)

	task := runningTaskForRetry(0, DefaultMaxTaskAttempts)
	statuses := []string{}
	for i := 0; i < 10; i++ {
		status, attempt := tr.recordTaskFailure(context.Background(), task)
		statuses = append(statuses, status)
		if status == "blocked" {
			break
		}
		// Simulate the re-dispatch: the runner reads the persisted counter
		// back onto the next run's record.
		task.AttemptCount = attempt
	}

	want := []string{"pending", "pending", "blocked"}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %v, want %v (loop did not terminate at the cap)", statuses, want)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", statuses, want)
		}
	}
}
