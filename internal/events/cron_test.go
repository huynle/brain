package events

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/pkg/cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockScheduleSource implements ScheduleSource for testing.
type mockScheduleSource struct {
	mu      sync.Mutex
	entries []ScheduleEntry
	err     error
}

func (m *mockScheduleSource) ListScheduledEntries(ctx context.Context) ([]ScheduleEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	result := make([]ScheduleEntry, len(m.entries))
	copy(result, m.entries)
	return result, nil
}

func TestCronEmitter_PublishesScheduleFired(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:        "task123",
				Path:      "projects/test/task/task123.md",
				ProjectID: "test",
				Schedule:  "* * * * *",
				Timezone:  "",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		events <- e
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	select {
	case e := <-events:
		assert.Equal(t, ScheduleFired, e.Type)
		assert.Equal(t, "task123", e.Payload["automation_id"])
		assert.Equal(t, "projects/test/task/task123.md", e.Payload["path"])
		assert.Equal(t, "test", e.ProjectID)
		assert.Equal(t, "cron_emitter", e.Source)
		assert.NotEmpty(t, e.DedupKey)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for schedule.fired event")
	}
}

func TestCronEmitter_DedupKey(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	key := dedupKey("task123", now)
	assert.Equal(t, "task123:2025-06-15T10:30Z", key)
}

func TestCronEmitter_DedupPreventsDoublePublish(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "task1",
				Path:     "projects/test/task/task1.md",
				Schedule: "* * * * *",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bucket by dedup key rather than summing. The guarantee the emitter
	// implements is "at most one event per (automation_id, fire-MINUTE)":
	// dedupKey formats fireTime to minute precision, so a run whose window
	// straddles :59 -> :00 correctly produces a SECOND event. Asserting a
	// total of 1 therefore encoded an invariant stronger than the code's,
	// and whether it held depended on the arbitrary wall-clock phase the
	// test happened to start at — roughly a 0.5% failure rate that parallel
	// load amplifies by stretching the window.
	//
	// This is not a widened tolerance: a minute rollover now yields two keys
	// with one event each and passes, while a genuine dedup regression
	// yields a key with two events and fails on the first duplicate tick —
	// strictly MORE sensitive than the old total, which could not tell the
	// two apart.
	var mu sync.Mutex
	counts := make(map[string]int)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		mu.Lock()
		counts[e.DedupKey]++
		mu.Unlock()
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	// Let it tick several times within the same minute
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Allow goroutines to finish
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, counts, "expected at least one schedule.fired event")
	for key, n := range counts {
		assert.Equal(t, 1, n, "dedup should prevent duplicate events for fire_time %s", key)
	}
}

func TestCronEmitter_GracefulShutdown(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "task1",
				Path:     "projects/test/task/task1.md",
				Schedule: "* * * * *",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		emitter.Start(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(2 * time.Second):
		t.Fatal("emitter did not shut down within timeout")
	}
}

func TestCronEmitter_FiltersNonMatchingSchedules(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	// Schedule that only fires at midnight on Jan 1
	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "task1",
				Path:     "projects/test/task/task1.md",
				Schedule: "0 0 1 1 *",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		events <- e
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	select {
	case <-events:
		now := time.Now().UTC()
		if now.Month() != 1 || now.Day() != 1 || now.Hour() != 0 {
			t.Fatal("should not have fired schedule.fired event for non-matching cron")
		}
	case <-time.After(300 * time.Millisecond):
		// Expected: no events
	}
}

func TestCronEmitter_HandlesInvalidCronExpression(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "bad-task",
				Path:     "projects/test/task/bad.md",
				Schedule: "invalid cron",
			},
			{
				ID:       "good-task",
				Path:     "projects/test/task/good.md",
				Schedule: "* * * * *",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		events <- e
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	select {
	case e := <-events:
		assert.Equal(t, "good-task", e.Payload["automation_id"])
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event from good task")
	}
}

func TestCronEmitter_TimezoneSupport(t *testing.T) {
	now := time.Now().UTC()
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	nowLocal := now.In(loc)
	schedule := formatMinuteHourCron(nowLocal)

	sched, err := cron.Parse(schedule)
	require.NoError(t, err)

	assert.True(t, sched.Matches(nowLocal), "schedule should match current time in local timezone")
}

func TestCronEmitter_TimezoneEvaluation(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	// Create a schedule matching the current time in America/New_York
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	nowLocal := time.Now().In(loc)
	schedule := formatMinuteHourCron(nowLocal)

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "tz-task",
				Path:     "projects/test/task/tz.md",
				Schedule: schedule,
				Timezone: "America/New_York",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		events <- e
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	select {
	case e := <-events:
		assert.Equal(t, "tz-task", e.Payload["automation_id"])
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for timezone-aware event")
	}
}

func TestCronEmitter_DefaultTickInterval(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:    bus,
		Source: src,
	})

	assert.Equal(t, 30*time.Second, emitter.tickInterval)
}

func TestCronEmitter_NextRunIncludedInPayload(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	src := &mockScheduleSource{
		entries: []ScheduleEntry{
			{
				ID:       "task1",
				Path:     "projects/test/task/task1.md",
				Schedule: "* * * * *",
			},
		},
	}

	emitter := NewCronEmitter(CronEmitterConfig{
		Bus:          bus,
		Source:       src,
		TickInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	sub := bus.Subscribe(ScheduleFired, func(e Event) {
		events <- e
	})
	defer sub.Unsubscribe()

	go emitter.Start(ctx)

	select {
	case e := <-events:
		nextRun, ok := e.Payload["next_run"]
		assert.True(t, ok, "payload should include next_run")
		assert.NotEmpty(t, nextRun, "next_run should not be empty")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestCronEmitter_CleansFiredKeys(t *testing.T) {
	emitter := &CronEmitter{
		firedKeys: map[string]time.Time{
			"old-key":    time.Now().Add(-3 * time.Hour),
			"recent-key": time.Now().Add(-30 * time.Minute),
		},
	}

	emitter.cleanFiredKeys(time.Now())

	assert.NotContains(t, emitter.firedKeys, "old-key", "old key should be cleaned")
	assert.Contains(t, emitter.firedKeys, "recent-key", "recent key should remain")
}

func TestCronEmitter_LoadTimezone(t *testing.T) {
	tests := []struct {
		name     string
		timezone string
		wantUTC  bool
	}{
		{"empty defaults to UTC", "", true},
		{"invalid defaults to UTC", "Invalid/Zone", true},
		{"valid timezone", "America/New_York", false},
		{"UTC explicit", "UTC", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := loadTimezone(tt.timezone)
			if tt.wantUTC {
				assert.Equal(t, time.UTC, loc)
			} else {
				assert.NotEqual(t, time.UTC, loc)
			}
		})
	}
}

// formatMinuteHourCron creates a cron expression matching the given time's minute and hour.
func formatMinuteHourCron(t time.Time) string {
	return fmt.Sprintf("%d %d * * *", t.Minute(), t.Hour())
}
