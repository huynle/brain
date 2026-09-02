package types

import (
	"errors"
	"strings"
	"time"
)

// Reminder actions — what happens when a dated reminder's time arrives.
const (
	// ReminderActionNotify surfaces the reminder in the app and does nothing
	// else. This is the default, because it is the only action that cannot
	// have side effects the author did not ask for.
	ReminderActionNotify = "notify"
	// ReminderActionTask generates a pending task so an agent works the
	// reminder.
	ReminderActionTask = "task"
)

// ReminderActions enumerates the valid trigger actions.
var ReminderActions = []string{ReminderActionNotify, ReminderActionTask}

// Reminder repeat intervals. Empty means one-shot.
const (
	ReminderRepeatDaily   = "daily"
	ReminderRepeatWeekly  = "weekly"
	ReminderRepeatMonthly = "monthly"
	ReminderRepeatYearly  = "yearly"
)

// ReminderRepeats enumerates the valid recurrence values.
var ReminderRepeats = []string{
	ReminderRepeatDaily, ReminderRepeatWeekly,
	ReminderRepeatMonthly, ReminderRepeatYearly,
}

// NormalizeReminderRepeat maps an input repeat to a stored one. Empty is
// valid and means one-shot. An unrecognized value returns "!" so the caller
// can reject it rather than silently making the reminder one-shot — a
// reminder that was meant to recur and does not is a silent failure.
func NormalizeReminderRepeat(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "":
		return ""
	case ReminderRepeatDaily, ReminderRepeatWeekly,
		ReminderRepeatMonthly, ReminderRepeatYearly:
		return v
	default:
		return "!"
	}
}

// Reminder tags. ReminderDatedTag is load-bearing, not decorative: the sweeper
// lists by tag, so an undated reminder is never even loaded rather than being
// loaded and skipped.
const (
	ReminderTag      = "reminder"
	ReminderDatedTag = "reminder:dated"
)

// ReminderIDTag is the per-reminder lookup tag, mirroring goal:<id>.
func ReminderIDTag(id string) string { return "reminder:" + id }

// ErrReminderNotFound is returned by the reminder service when no entry
// carries the requested id, in any status.
var ErrReminderNotFound = errors.New("reminder not found")

// ReminderConfig is the nested `reminder:` frontmatter block.
//
// Modelled on GoalConfig deliberately: one nested struct is ONE key to
// register in pkg/frontmatter's knownFields rather than a dozen flat fields,
// each of which would be an independent chance to land in Frontmatter.Extra
// where nothing reads it.
type ReminderConfig struct {
	ID string `json:"id" yaml:"id"`

	// RemindAt is when to fire, RFC3339 WITH an explicit offset. Empty means
	// undated — "just something to come back to" — which never fires and is
	// never loaded by the sweeper.
	RemindAt string `json:"remind_at,omitempty" yaml:"remind_at,omitempty"`

	// Timezone is an IANA name kept for display only. Comparison is always in
	// UTC against RemindAt's own offset: there is no server-wide timezone
	// setting anywhere in this codebase, so a naive wall-clock time would have
	// nothing to resolve against and would fire at the wrong hour.
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`

	// Action is notify (default) or task.
	Action string `json:"action,omitempty" yaml:"action,omitempty"`

	// Prompt is the instruction handed to the generated task. Required when
	// Action is task — a task with no prompt is not work, it is noise.
	Prompt string `json:"prompt,omitempty" yaml:"prompt,omitempty"`

	// Execution hints copied onto the generated task, all optional.
	Agent         string `json:"agent,omitempty" yaml:"agent,omitempty"`
	Model         string `json:"model,omitempty" yaml:"model,omitempty"`
	Executor      string `json:"executor,omitempty" yaml:"executor,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty" yaml:"execution_mode,omitempty"`
	TargetWorkdir string `json:"target_workdir,omitempty" yaml:"target_workdir,omitempty"`

	// Repeat makes the reminder recur: daily, weekly, monthly, yearly.
	// Empty is one-shot.
	Repeat string `json:"repeat,omitempty" yaml:"repeat,omitempty"`

	// RepeatUntil stops a recurrence (RFC3339). Empty means forever.
	RepeatUntil string `json:"repeat_until,omitempty" yaml:"repeat_until,omitempty"`

	// FireCount counts how many times this reminder has fired. Useful for a
	// recurring reminder, where FiredAt only records the most recent one.
	FireCount int `json:"fire_count,omitempty" yaml:"fire_count,omitempty"`

	// FiredAt records when this reminder fired, for display. It is NOT the
	// exactly-once guard — that is the event log's unique dedup key, which is
	// atomic and cannot be lost to a re-index.
	FiredAt string `json:"fired_at,omitempty" yaml:"fired_at,omitempty"`

	// GeneratedTaskID names the task an Action=task firing created.
	GeneratedTaskID string `json:"generated_task_id,omitempty" yaml:"generated_task_id,omitempty"`
}

// NormalizeReminderAction maps an input action to a stored one. Empty means
// notify. An unrecognized value returns "" so the caller can reject it rather
// than quietly doing something the author did not ask for.
func NormalizeReminderAction(s string) string {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "":
		return ReminderActionNotify
	case ReminderActionNotify:
		return ReminderActionNotify
	case ReminderActionTask:
		return ReminderActionTask
	default:
		return ""
	}
}

// IsValidReminderAction reports whether s names a real action.
func IsValidReminderAction(s string) bool {
	return NormalizeReminderAction(s) != ""
}

// IsDated reports whether this reminder has a firing time at all.
func (r *ReminderConfig) IsDated() bool {
	return r != nil && strings.TrimSpace(r.RemindAt) != ""
}

// RemindAtTime parses RemindAt.
func (r *ReminderConfig) RemindAtTime() (time.Time, error) {
	if r == nil || strings.TrimSpace(r.RemindAt) == "" {
		return time.Time{}, errors.New("reminder has no remind_at")
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(r.RemindAt))
}

// NormalizedAction returns the effective action, defaulting to notify.
func (r *ReminderConfig) NormalizedAction() string {
	if r == nil {
		return ReminderActionNotify
	}
	if a := NormalizeReminderAction(r.Action); a != "" {
		return a
	}
	return ReminderActionNotify
}

// Repeats reports whether this reminder recurs.
func (r *ReminderConfig) Repeats() bool {
	return r != nil && strings.TrimSpace(r.Repeat) != ""
}

// NextOccurrence advances t by one repeat interval.
//
// Advancing from the SCHEDULED time rather than from "now" is what keeps a
// daily 09:00 reminder at 09:00: computing from the firing instant would walk
// it later every time the sweeper ticked a few seconds late.
//
// AddDate is used for month and year so "the 31st" clamps the way the
// calendar does rather than drifting by a fixed number of days.
func (r *ReminderConfig) NextOccurrence(t time.Time) (time.Time, bool) {
	switch NormalizeReminderRepeat(r.Repeat) {
	case ReminderRepeatDaily:
		return t.AddDate(0, 0, 1), true
	case ReminderRepeatWeekly:
		return t.AddDate(0, 0, 7), true
	case ReminderRepeatMonthly:
		return t.AddDate(0, 1, 0), true
	case ReminderRepeatYearly:
		return t.AddDate(1, 0, 0), true
	default:
		return time.Time{}, false
	}
}

// RepeatEnded reports whether the recurrence has passed its RepeatUntil.
func (r *ReminderConfig) RepeatEnded(next time.Time) bool {
	if r == nil || strings.TrimSpace(r.RepeatUntil) == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(r.RepeatUntil))
	if err != nil {
		return false
	}
	return next.After(until)
}

// Reminder lifecycle states, derived from the entry's status plus whether it
// carries a date. Reported by the API so callers never have to re-derive it.
const (
	ReminderStateArmed   = "armed"   // dated, active, waiting
	ReminderStateUndated = "undated" // no date; will never fire on its own
	ReminderStateFired   = "fired"   // fired and not yet acknowledged
	ReminderStateDone    = "done"    // acknowledged
	ReminderStatePaused  = "paused"  // held; will not fire
)

// CreateReminderRequest is the body of POST /reminders.
type CreateReminderRequest struct {
	Project   string         `json:"project,omitempty"`
	Global    *bool          `json:"global,omitempty"`
	FeatureID string         `json:"feature_id,omitempty"`
	Title     string         `json:"title"`
	Content   string         `json:"content,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Config    ReminderConfig `json:"config"`
}

// UpdateReminderRequest is the body of PATCH /reminders/{id}.
//
// Every field is a POINTER so the handler can distinguish "not supplied" from
// "supplied as empty". That distinction is the only way to express "clear this
// reminder's date", i.e. turn a dated reminder back into an undated one.
type UpdateReminderRequest struct {
	Title   *string `json:"title,omitempty"`
	Content *string `json:"content,omitempty"`
	Status  *string `json:"status,omitempty"`

	RemindAt      *string `json:"remind_at,omitempty"`
	Timezone      *string `json:"timezone,omitempty"`
	Repeat        *string `json:"repeat,omitempty"`
	RepeatUntil   *string `json:"repeat_until,omitempty"`
	Action        *string `json:"action,omitempty"`
	Prompt        *string `json:"prompt,omitempty"`
	Agent         *string `json:"agent,omitempty"`
	Model         *string `json:"model,omitempty"`
	Executor      *string `json:"executor,omitempty"`
	ExecutionMode *string `json:"execution_mode,omitempty"`
	TargetWorkdir *string `json:"target_workdir,omitempty"`
}

// ReminderSummary is the API view of one reminder.
type ReminderSummary struct {
	EntryID    string `json:"entry_id"`
	ReminderID string `json:"reminder_id"`
	Title      string `json:"title"`
	Project    string `json:"project,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
	Path       string `json:"path,omitempty"`

	Status string `json:"status"`
	State  string `json:"state"`

	RemindAt    string `json:"remind_at,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Action      string `json:"action"`
	Prompt      string `json:"prompt,omitempty"`
	Repeat      string `json:"repeat,omitempty"`
	RepeatUntil string `json:"repeat_until,omitempty"`
	FireCount   int    `json:"fire_count,omitempty"`

	FiredAt         string `json:"fired_at,omitempty"`
	GeneratedTaskID string `json:"generated_task_id,omitempty"`

	// LateBySeconds is how far past remind_at the firing actually happened —
	// non-zero after an outage, and the only way to tell a punctual reminder
	// from one that drained hours later.
	LateBySeconds int64 `json:"late_by_seconds,omitempty"`
}

// ReminderListResponse is the body of GET /reminders.
type ReminderListResponse struct {
	Reminders []ReminderSummary `json:"reminders"`
	Count     int               `json:"count"`
}

// ReminderFiredPayload is the event payload for reminder.fired.
type ReminderFiredPayload struct {
	ReminderID      string `json:"reminder_id"`
	EntryID         string `json:"entry_id"`
	Project         string `json:"project,omitempty"`
	Title           string `json:"title"`
	Action          string `json:"action"`
	RemindAt        string `json:"remind_at"`
	FiredAt         string `json:"fired_at"`
	LateBySeconds   int64  `json:"late_by_seconds,omitempty"`
	GeneratedTaskID string `json:"generated_task_id,omitempty"`
	// NextRemindAt is set for a recurring reminder: when it will fire again.
	NextRemindAt string `json:"next_remind_at,omitempty"`
}
