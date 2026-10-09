package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
)

const (
	// schedulingReportPageSize is the page size for each list read.
	schedulingReportPageSize = 500
	// schedulingReportEntryCap bounds how many entries of one type are scanned.
	schedulingReportEntryCap = 10000
	// schedulingReportDedupPrefix prefixes the content-hash dedup key.
	schedulingReportDedupPrefix = "scheduling-report:"
	schedulingReportKind        = "scheduling_report"
)

// SchedulingEntryLister is the read the startup scheduling report scans.
// *BrainServiceImpl satisfies it.
type SchedulingEntryLister interface {
	List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)
}

// SchedulingReport lists entries whose scheduling meaning changed in this
// release. Findings are ordered by section, then entry path, so the same
// content always renders the same body.
type SchedulingReport struct {
	sections []reportSection
	notes    []string
	count    int
}

type reportSection struct {
	heading string
	lines   []string
}

// Count reports the number of findings, not counting notes.
func (r SchedulingReport) Count() int { return r.count }

// Empty reports whether there is nothing to raise: no findings and no
// truncation note.
func (r SchedulingReport) Empty() bool { return r.count == 0 && len(r.notes) == 0 }

// Body renders the report as plain text. An empty report renders as "".
func (r SchedulingReport) Body() string {
	if r.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("Scheduling semantics changed in this release. Review each item below.\n")
	for _, sec := range r.sections {
		if len(sec.lines) == 0 {
			continue
		}
		b.WriteString("\n" + sec.heading + "\n")
		for _, line := range sec.lines {
			b.WriteString("- " + line + "\n")
		}
	}
	if len(r.notes) > 0 {
		b.WriteString("\nNotes:\n")
		for _, note := range r.notes {
			b.WriteString("- " + note + "\n")
		}
	}
	return b.String()
}

// BuildSchedulingReport scans automations (all statuses except archived) and
// tasks. It reports cron expressions whose day fields now combine with OR,
// automations whose lifecycle fields are now enforced, and filter or match
// values that already begin with "re:". A nil lister yields an empty report.
func BuildSchedulingReport(ctx context.Context, lister SchedulingEntryLister) (SchedulingReport, error) {
	if lister == nil {
		return SchedulingReport{}, nil
	}
	automations, autoTruncated, err := listSchedulingEntries(ctx, lister, "automation")
	if err != nil {
		return SchedulingReport{}, fmt.Errorf("scan automations: %w", err)
	}
	tasks, taskTruncated, err := listSchedulingEntries(ctx, lister, "task")
	if err != nil {
		return SchedulingReport{}, fmt.Errorf("scan tasks: %w", err)
	}
	sortSchedulingEntries(automations)
	sortSchedulingEntries(tasks)

	var schedule, lifecycle, regex []string
	for _, a := range automations {
		if a.Status == "archived" {
			continue
		}
		if a.Trigger != nil {
			if expr := strings.TrimSpace(a.Trigger.Schedule); schedulingDayFieldsBothRestricted(expr) {
				schedule = append(schedule, fmt.Sprintf("automation %s %q: trigger.schedule %q", a.Path, a.Title, expr))
			}
			regex = append(regex, schedulingRegexLines(a)...)
		}
		if line := schedulingLifecycleLine(a); line != "" {
			lifecycle = append(lifecycle, line)
		}
	}
	for _, t := range tasks {
		if expr := strings.TrimSpace(t.Schedule); schedulingDayFieldsBothRestricted(expr) {
			schedule = append(schedule, fmt.Sprintf("task %s %q: schedule %q", t.Path, t.Title, expr))
		}
		if expr := strings.TrimSpace(t.FeatureSchedule); schedulingDayFieldsBothRestricted(expr) {
			schedule = append(schedule, fmt.Sprintf("task %s %q: feature_schedule %q", t.Path, t.Title, expr))
		}
	}

	report := SchedulingReport{
		sections: []reportSection{
			{heading: "Day-of-month and day-of-week now combine with OR when both are restricted:", lines: schedule},
			{heading: "Lifecycle fields are now enforced on automations (starts_at, expires_at, max_runs):", lines: lifecycle},
			{heading: "Filter and match values starting with re: are now regular expressions:", lines: regex},
		},
		count: len(schedule) + len(lifecycle) + len(regex),
	}
	if autoTruncated {
		report.notes = append(report.notes, fmt.Sprintf("automation scan truncated at %d entries; later automations were not checked", schedulingReportEntryCap))
	}
	if taskTruncated {
		report.notes = append(report.notes, fmt.Sprintf("task scan truncated at %d entries; later tasks were not checked", schedulingReportEntryCap))
	}
	return report, nil
}

// RaiseSchedulingReport builds the report, logs it, and notifies once. The
// dedup key is a hash of the body, so an identical report is not re-raised
// after a restart. It reports whether the report was non-empty. A nil lister
// or notifier is a no-op.
func RaiseSchedulingReport(ctx context.Context, lister SchedulingEntryLister, notifier SystemNotifier) (bool, error) {
	if lister == nil {
		return false, nil
	}
	report, err := BuildSchedulingReport(ctx, lister)
	if err != nil {
		return false, err
	}
	if report.Empty() {
		return false, nil
	}
	body := report.Body()
	slog.Warn("startup scheduling report: schedule semantics changed",
		"findings", report.Count(), "truncated", len(report.notes) > 0, "report", body)

	sum := sha256.Sum256([]byte(body))
	title := fmt.Sprintf("Scheduling semantics changed: %d item(s) to review", report.Count())
	if report.Count() == 0 {
		title = "Scheduling report: scan truncated"
	}
	notice := SystemNotice{
		Kind:       schedulingReportKind,
		Severity:   types.AttentionSeverityWarning,
		Title:      title,
		Body:       body,
		SourceType: "system",
		DedupKey:   schedulingReportDedupPrefix + hex.EncodeToString(sum[:]),
	}
	if err := NotifySystem(ctx, notifier, notice); err != nil {
		return true, err
	}
	return true, nil
}

// StartSchedulingReport runs RaiseSchedulingReport once in a background
// goroutine, after ready is closed (the boot index scan has finished, so the
// report never reads a partial index). It never fails startup: errors and
// panics are logged. A nil lister starts nothing.
func StartSchedulingReport(ctx context.Context, ready <-chan struct{}, lister SchedulingEntryLister, notifier SystemNotifier) {
	if lister == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("startup scheduling report panicked; startup continues", "panic", r)
			}
		}()
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		if _, err := RaiseSchedulingReport(ctx, lister, notifier); err != nil {
			slog.Error("startup scheduling report failed; startup continues", "error", err)
		}
	}()
}

// listSchedulingEntries pages through one entry type up to the cap. It also
// reports whether more entries existed beyond the cap.
func listSchedulingEntries(ctx context.Context, lister SchedulingEntryLister, entryType string) ([]types.BrainEntry, bool, error) {
	var all []types.BrainEntry
	for {
		if len(all) >= schedulingReportEntryCap {
			probe, err := lister.List(ctx, types.ListEntriesRequest{Type: entryType, Limit: 1, Offset: len(all)})
			if err != nil {
				return nil, false, err
			}
			return all, len(probe.Entries) > 0, nil
		}
		limit := min(schedulingReportPageSize, schedulingReportEntryCap-len(all))
		page, err := lister.List(ctx, types.ListEntriesRequest{Type: entryType, Limit: limit, Offset: len(all)})
		if err != nil {
			return nil, false, err
		}
		all = append(all, page.Entries...)
		if len(page.Entries) < limit {
			return all, false, nil
		}
	}
}

// schedulingDayFieldsBothRestricted reports whether expr is a cron expression
// whose day-of-month and day-of-week both restrict the day, so its meaning
// changed from AND to OR. Unparseable expressions are not reported.
func schedulingDayFieldsBothRestricted(expr string) bool {
	if expr == "" {
		return false
	}
	sched, err := cron.Parse(expr)
	if err != nil {
		return false
	}
	return sched.DayFieldsBothRestricted()
}

// schedulingLifecycleLine describes the lifecycle fields an automation carries.
// It returns "" when it carries none. A max_runs of 0 is still a set value.
func schedulingLifecycleLine(a types.BrainEntry) string {
	var fields []string
	if a.StartsAt != "" {
		fields = append(fields, fmt.Sprintf("starts_at %q", a.StartsAt))
	}
	if a.ExpiresAt != "" {
		fields = append(fields, fmt.Sprintf("expires_at %q", a.ExpiresAt))
	}
	if a.MaxRuns != nil {
		fields = append(fields, fmt.Sprintf("max_runs %d", *a.MaxRuns))
	}
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("automation %s %q: %s", a.Path, a.Title, strings.Join(fields, ", "))
}

// schedulingRegexLines lists the trigger filter and match values that already
// begin with "re:", keys sorted, filter before match.
func schedulingRegexLines(a types.BrainEntry) []string {
	var lines []string
	for _, group := range []struct {
		name   string
		values map[string]string
	}{
		{"filter", a.Trigger.Filter},
		{"match", a.Trigger.Match},
	} {
		keys := make([]string, 0, len(group.values))
		for k := range group.values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v := group.values[k]; strings.HasPrefix(v, "re:") {
				lines = append(lines, fmt.Sprintf("automation %s %q: trigger.%s.%s %q", a.Path, a.Title, group.name, k, v))
			}
		}
	}
	return lines
}

func sortSchedulingEntries(entries []types.BrainEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
}
