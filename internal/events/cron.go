// Package events — CronEmitter: centralized cron schedule evaluator.
//
// CronEmitter runs as a background goroutine in the brain server,
// scanning automation/task entries with cron schedules, evaluating
// expressions, and publishing schedule.fired events with dedup.
package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/huynle/brain-api/pkg/cron"
)

// ScheduleEntry represents a brain entry with a cron schedule.
// The CronEmitter evaluates these to determine when to fire events.
type ScheduleEntry struct {
	ID        string // Short 8-char ID of the entry
	Path      string // Full path (e.g., "projects/brain-api/task/abc123.md")
	ProjectID string // Project scope
	Schedule  string // 5-field cron expression
	Timezone  string // IANA timezone (empty = UTC)
}

// ScheduleSource provides the CronEmitter with scheduled entries to evaluate.
// Implementations query the storage layer for entries with cron schedules.
type ScheduleSource interface {
	// ListScheduledEntries returns all entries that have active cron schedules.
	ListScheduledEntries(ctx context.Context) ([]ScheduleEntry, error)
}

// CronEmitterConfig holds configuration for creating a CronEmitter.
type CronEmitterConfig struct {
	Bus          Bus            // Event bus to publish schedule.fired events
	Source       ScheduleSource // Provides scheduled entries to evaluate
	TickInterval time.Duration  // How often to evaluate schedules (default: 30s)
}

// CronEmitter periodically evaluates cron schedules and publishes
// schedule.fired events on the event bus. It tracks next_run per
// automation and deduplicates fires using automation_id + fire_time.
type CronEmitter struct {
	bus          Bus
	source       ScheduleSource
	tickInterval time.Duration

	// firedKeys tracks dedup keys that have already been published
	// to prevent duplicate events for the same fire_time.
	mu        sync.Mutex
	firedKeys map[string]time.Time // dedupKey -> when it was recorded
}

// NewCronEmitter creates a new CronEmitter with the given configuration.
// Uses default tick interval of 30s if not specified.
func NewCronEmitter(cfg CronEmitterConfig) *CronEmitter {
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = 30 * time.Second
	}

	return &CronEmitter{
		bus:          cfg.Bus,
		source:       cfg.Source,
		tickInterval: tick,
		firedKeys:    make(map[string]time.Time),
	}
}

// Start begins the cron evaluation loop. It blocks until ctx is cancelled.
// Safe to call from a goroutine.
func (ce *CronEmitter) Start(ctx context.Context) {
	slog.Info("cron_emitter: starting", "tick_interval", ce.tickInterval)

	// Run an immediate evaluation on startup
	ce.evaluate(ctx)

	ticker := time.NewTicker(ce.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("cron_emitter: shutting down")
			return
		case <-ticker.C:
			ce.evaluate(ctx)
		}
	}
}

// evaluate performs a single cron evaluation cycle:
// 1. Fetch all scheduled entries
// 2. For each, check if the cron expression matches the current time
// 3. If it matches and hasn't been fired for this fire_time, publish schedule.fired
func (ce *CronEmitter) evaluate(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	entries, err := ce.source.ListScheduledEntries(ctx)
	if err != nil {
		slog.Warn("cron_emitter: failed to list scheduled entries", "error", err)
		return
	}

	now := time.Now().UTC()

	for _, entry := range entries {
		ce.evaluateEntry(entry, now)
	}

	// Periodically clean old dedup keys (older than 2 hours)
	ce.cleanFiredKeys(now)
}

// evaluateEntry checks a single entry's cron expression against the current time.
func (ce *CronEmitter) evaluateEntry(entry ScheduleEntry, now time.Time) {
	sched, err := cron.Parse(entry.Schedule)
	if err != nil {
		slog.Debug("cron_emitter: invalid cron expression",
			"id", entry.ID,
			"schedule", entry.Schedule,
			"error", err,
		)
		return
	}

	// Evaluate in the entry's timezone
	loc := loadTimezone(entry.Timezone)
	nowLocal := now.In(loc)

	if !sched.Matches(nowLocal) {
		return
	}

	// Build dedup key: automation_id + fire_time (truncated to minute)
	fireTime := nowLocal.Truncate(time.Minute)
	key := dedupKey(entry.ID, fireTime)

	// Check if already fired for this fire_time
	ce.mu.Lock()
	if _, fired := ce.firedKeys[key]; fired {
		ce.mu.Unlock()
		return
	}
	ce.firedKeys[key] = now
	ce.mu.Unlock()

	// Compute next_run
	nextRun := sched.NextAfter(nowLocal)
	var nextRunStr string
	if !nextRun.IsZero() {
		nextRunStr = nextRun.UTC().Format(time.RFC3339)
	}

	// Publish schedule.fired event
	ce.bus.Publish(Event{
		Type:      ScheduleFired,
		Source:    "cron_emitter",
		ProjectID: entry.ProjectID,
		DedupKey:  key,
		Payload: map[string]any{
			"automation_id": entry.ID,
			"path":          entry.Path,
			"schedule":      entry.Schedule,
			"fire_time":     fireTime.UTC().Format(time.RFC3339),
			"next_run":      nextRunStr,
		},
	})

	slog.Info("cron_emitter: schedule.fired",
		"id", entry.ID,
		"schedule", entry.Schedule,
		"fire_time", fireTime.UTC().Format(time.RFC3339),
		"next_run", nextRunStr,
	)
}

// cleanFiredKeys removes dedup entries older than 2 hours to prevent
// unbounded memory growth.
func (ce *CronEmitter) cleanFiredKeys(now time.Time) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	cutoff := now.Add(-2 * time.Hour)
	for key, recorded := range ce.firedKeys {
		if recorded.Before(cutoff) {
			delete(ce.firedKeys, key)
		}
	}
}

// dedupKey builds a deduplication key from automation ID and fire time.
// Format: "automation_id:2025-06-15T10:30Z"
func dedupKey(automationID string, fireTime time.Time) string {
	return fmt.Sprintf("%s:%s", automationID, fireTime.UTC().Format("2006-01-02T15:04Z"))
}

// loadTimezone loads an IANA timezone location.
// Returns UTC if timezone is empty or invalid.
func loadTimezone(timezone string) *time.Location {
	if timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		slog.Warn("cron_emitter: invalid timezone, falling back to UTC",
			"timezone", timezone,
			"error", err,
		)
		return time.UTC
	}
	return loc
}
