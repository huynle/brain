package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/attentionstore"
	"github.com/huynle/brain-api/internal/types"
)

// fakeEntryLister serves entries per type with the same offset/limit paging
// shape as BrainServiceImpl.List.
type fakeEntryLister struct {
	byType map[string][]types.BrainEntry
	calls  atomic.Int64
	err    error
}

func (f *fakeEntryLister) List(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	all := f.byType[req.Type]
	start := min(req.Offset, len(all))
	end := len(all)
	if req.Limit > 0 && start+req.Limit < end {
		end = start + req.Limit
	}
	return &types.ListEntriesResponse{Entries: all[start:end], Total: len(all), Limit: req.Limit, Offset: req.Offset}, nil
}

// recordingNotifier captures every notice it is asked to raise.
type recordingNotifier struct{ notices []SystemNotice }

func (r *recordingNotifier) Notify(_ context.Context, n SystemNotice) error {
	r.notices = append(r.notices, n)
	return nil
}

// findingLine returns the report line that names path, or "" when none does.
func findingLine(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, path) {
			return line
		}
	}
	return ""
}

func TestSchedulingReportFlagsDayFieldsBothRestricted(t *testing.T) {
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {
			{Path: "projects/canis/automation/both.md", Title: "Both", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}},
			{Path: "projects/canis/automation/dow.md", Title: "Weekdays", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 * * 1"}},
			{Path: "projects/canis/automation/stepped.md", Title: "Stepped", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 */2 * 1"}},
			{Path: "projects/canis/automation/notcron.md", Title: "Bad", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "not a cron"}},
		},
		"task": {
			{Path: "projects/canis/task/t1.md", Title: "Recurring", Type: "task", Status: "pending",
				Schedule: "0 3 15 * 7", FeatureSchedule: "0 3 */2 * 1"},
			{Path: "projects/canis/task/t2.md", Title: "Feature", Type: "task", Status: "pending",
				FeatureSchedule: "0 3 1 * 1"},
		},
	}}

	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	body := report.Body()

	for _, want := range []struct{ path, expr string }{
		{"projects/canis/automation/both.md", "0 3 1 * 1"},
		{"projects/canis/task/t1.md", "0 3 15 * 7"},
		{"projects/canis/task/t2.md", "0 3 1 * 1"},
	} {
		line := findingLine(body, want.path)
		if line == "" || !strings.Contains(line, want.expr) || !strings.Contains(line, "schedule") {
			t.Errorf("finding for %s = %q, want the expression %q", want.path, line, want.expr)
		}
	}
	for _, path := range []string{
		"projects/canis/automation/dow.md",
		"projects/canis/automation/stepped.md",
		"projects/canis/automation/notcron.md",
	} {
		if line := findingLine(body, path); line != "" {
			t.Errorf("%s should not be flagged, got %q", path, line)
		}
	}
	if line := findingLine(body, "projects/canis/task/t1.md"); strings.Contains(line, "feature_schedule") {
		t.Errorf("t1 feature_schedule */2 must not be flagged, got %q", line)
	}
}

func TestSchedulingReportFlagsEnforcedLifecycleFieldsOnAutomationsOnly(t *testing.T) {
	three, zero := 3, 0
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {
			{Path: "projects/canis/automation/life.md", Title: "Life", Type: "automation", Status: "active",
				StartsAt: "2026-11-01T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z", MaxRuns: &three},
			{Path: "projects/canis/automation/maxonly.md", Title: "Max", Type: "automation", Status: "active",
				MaxRuns: &zero},
			{Path: "projects/canis/automation/plain.md", Title: "Plain", Type: "automation", Status: "active"},
		},
		"task": {
			{Path: "projects/canis/task/started.md", Title: "Task", Type: "task", Status: "pending",
				StartsAt: "2026-11-01T00:00:00Z"},
		},
	}}

	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	body := report.Body()

	life := findingLine(body, "projects/canis/automation/life.md")
	for _, want := range []string{"starts_at", "expires_at", "max_runs", "2026-11-01T00:00:00Z", "3"} {
		if !strings.Contains(life, want) {
			t.Errorf("lifecycle finding %q missing %q", life, want)
		}
	}
	if line := findingLine(body, "projects/canis/automation/maxonly.md"); !strings.Contains(line, "max_runs") {
		t.Errorf("max_runs: 0 is still a set value and must be flagged, got %q", line)
	}
	if line := findingLine(body, "projects/canis/automation/plain.md"); line != "" {
		t.Errorf("automation without lifecycle fields flagged: %q", line)
	}
	if line := findingLine(body, "projects/canis/task/started.md"); line != "" {
		t.Errorf("task starts_at is not an automation lifecycle field, got %q", line)
	}
}

func TestSchedulingReportFlagsReFilterAndMatchValues(t *testing.T) {
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {
			{Path: "projects/canis/automation/re.md", Title: "Regex", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{
					Type: "event",
					Filter: map[string]string{
						"title":     "re:(?i)^release",
						"to_status": "completed",
						"tags":      "has:re:x",
					},
					Match: map[string]string{"description": "re:docs"},
				}},
			{Path: "projects/canis/automation/plain.md", Title: "Plain", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "event", Filter: map[string]string{"title": "not re:x"}}},
		},
	}}

	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	body := report.Body()

	re := findingLine(body, "projects/canis/automation/re.md")
	if !strings.Contains(re, "trigger.filter.title") || !strings.Contains(re, "re:(?i)^release") {
		t.Errorf("filter re: finding = %q", re)
	}
	if !strings.Contains(body, "trigger.match.description") || !strings.Contains(body, "re:docs") {
		t.Errorf("match re: finding missing from report:\n%s", body)
	}
	if strings.Contains(body, "to_status") || strings.Contains(body, "has:re:x") {
		t.Errorf("non re: values must not be flagged:\n%s", body)
	}
	if line := findingLine(body, "projects/canis/automation/plain.md"); line != "" {
		t.Errorf("value containing re: mid-string flagged: %q", line)
	}
}

func TestSchedulingReportSkipsArchivedAutomations(t *testing.T) {
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {
			{Path: "projects/canis/automation/old.md", Title: "Old", Type: "automation", Status: "archived",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}},
		},
	}}
	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	if report.Count() != 0 {
		t.Fatalf("archived automation reported:\n%s", report.Body())
	}
}

func TestSchedulingReportWithNoFindingsRaisesNothing(t *testing.T) {
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {
			{Path: "projects/canis/automation/plain.md", Title: "Plain", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 * * 1"}},
		},
		"task": {{Path: "projects/canis/task/t.md", Title: "T", Type: "task", Status: "pending", Schedule: "0 3 * * 1"}},
	}}
	notifier := &recordingNotifier{}

	raised, err := RaiseSchedulingReport(context.Background(), lister, notifier)
	if err != nil {
		t.Fatal(err)
	}
	if raised || len(notifier.notices) != 0 {
		t.Fatalf("empty report raised=%v notices=%d, want nothing", raised, len(notifier.notices))
	}
}

func TestSchedulingReportDedupesIdenticalReportAcrossRestarts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "attention.db")
	flagged := func(paths ...string) *fakeEntryLister {
		var entries []types.BrainEntry
		for _, p := range paths {
			entries = append(entries, types.BrainEntry{Path: p, Title: "A", Type: "automation", Status: "active",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}})
		}
		return &fakeEntryLister{byType: map[string][]types.BrainEntry{"automation": entries}}
	}
	recipientItems := func(svc *AttentionService) []types.Attention {
		t.Helper()
		items, err := svc.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice", IncludeSnoozed: true})
		if err != nil {
			t.Fatal(err)
		}
		var out []types.Attention
		for _, it := range items {
			if it.Kind == "scheduling_report" {
				out = append(out, it)
			}
		}
		return out
	}

	// Boot 1 raises the report.
	store1, err := attentionstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc1 := NewAttentionService(store1)
	lister := flagged("projects/canis/automation/a.md")
	raised, err := RaiseSchedulingReport(ctx, lister, NewSystemNotifier(svc1, []string{"alice"}))
	if err != nil || !raised {
		t.Fatalf("boot 1: raised=%v err=%v, want raised", raised, err)
	}
	_ = store1.Close()

	// Boot 2 scans the same content: nothing new is raised.
	store2, err := attentionstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	svc2 := NewAttentionService(store2)
	if _, err := RaiseSchedulingReport(ctx, lister, NewSystemNotifier(svc2, []string{"alice"})); err != nil {
		t.Fatal(err)
	}
	items := recipientItems(svc2)
	if len(items) != 1 {
		t.Fatalf("identical report after restart produced %d items, want 1", len(items))
	}
	if want := "scheduling-report:"; !strings.HasPrefix(items[0].DedupKey, want) || len(items[0].DedupKey) != len(want)+64 {
		t.Fatalf("dedup key = %q, want %q + sha256 hex", items[0].DedupKey, want)
	}

	// Changed content is a different report and is raised.
	changed := flagged("projects/canis/automation/a.md", "projects/canis/automation/b.md")
	if _, err := RaiseSchedulingReport(ctx, changed, NewSystemNotifier(svc2, []string{"alice"})); err != nil {
		t.Fatal(err)
	}
	if got := len(recipientItems(svc2)); got != 2 {
		t.Fatalf("changed report produced %d items total, want 2", got)
	}
}

func TestSchedulingReportPagesThroughAllEntries(t *testing.T) {
	entries := make([]types.BrainEntry, 1200)
	for i := range entries {
		entries[i] = types.BrainEntry{
			Path: fmt.Sprintf("projects/p/automation/a%04d.md", i), Title: "A", Type: "automation", Status: "active",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"},
		}
	}
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{"automation": entries}}

	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	if report.Count() != 1200 {
		t.Fatalf("Count() = %d, want all 1200 entries across pages", report.Count())
	}
	if strings.Contains(report.Body(), "truncated") {
		t.Fatal("1200 entries are under the cap; no truncation note expected")
	}
	if lister.calls.Load() < 3 {
		t.Fatalf("list calls = %d, want at least 3 pages", lister.calls.Load())
	}
}

func TestSchedulingReportCapsEachTypeAndNotesTruncation(t *testing.T) {
	entries := make([]types.BrainEntry, 10001)
	for i := range entries {
		entries[i] = types.BrainEntry{
			Path: fmt.Sprintf("projects/p/automation/a%05d.md", i), Title: "A", Type: "automation", Status: "active",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"},
		}
	}
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{"automation": entries}}

	report, err := BuildSchedulingReport(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	if report.Count() != 10000 {
		t.Fatalf("Count() = %d, want the 10000-entry cap", report.Count())
	}
	if !strings.Contains(report.Body(), "truncated") {
		t.Fatalf("truncation not noted:\n%s", report.Body()[:min(400, len(report.Body()))])
	}
}

func TestSchedulingReportIsDeterministic(t *testing.T) {
	entries := []types.BrainEntry{
		{Path: "projects/b/automation/z.md", Title: "Z", Type: "automation", Status: "active",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}},
		{Path: "projects/a/automation/y.md", Title: "Y", Type: "automation", Status: "active",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}},
	}
	first, err := BuildSchedulingReport(context.Background(), &fakeEntryLister{byType: map[string][]types.BrainEntry{"automation": entries}})
	if err != nil {
		t.Fatal(err)
	}
	reversed := []types.BrainEntry{entries[1], entries[0]}
	second, err := BuildSchedulingReport(context.Background(), &fakeEntryLister{byType: map[string][]types.BrainEntry{"automation": reversed}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Body() != second.Body() {
		t.Fatalf("report depends on list order:\n%s\n---\n%s", first.Body(), second.Body())
	}
}

func TestSchedulingReportListErrorRaisesNothingAndDoesNotPanic(t *testing.T) {
	lister := &fakeEntryLister{err: errors.New("index unavailable")}
	notifier := &recordingNotifier{}
	raised, err := RaiseSchedulingReport(context.Background(), lister, notifier)
	if err == nil {
		t.Fatal("list failure must be reported to the caller for logging")
	}
	if raised || len(notifier.notices) != 0 {
		t.Fatalf("list failure raised=%v notices=%d, want nothing", raised, len(notifier.notices))
	}
}

func TestStartSchedulingReportWaitsForIndexReadiness(t *testing.T) {
	lister := &fakeEntryLister{byType: map[string][]types.BrainEntry{
		"automation": {{Path: "projects/canis/automation/a.md", Title: "A", Type: "automation", Status: "active",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"}}},
	}}
	raised := make(chan SystemNotice, 1)
	notifier := notifierFunc(func(n SystemNotice) { raised <- n })
	ready := make(chan struct{})

	StartSchedulingReport(context.Background(), ready, lister, notifier)

	time.Sleep(50 * time.Millisecond)
	if got := lister.calls.Load(); got != 0 {
		t.Fatalf("report scanned %d times before the index was ready", got)
	}
	close(ready)
	select {
	case n := <-raised:
		if n.Kind != "scheduling_report" || !strings.HasPrefix(n.DedupKey, "scheduling-report:") {
			t.Fatalf("notice = %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report was not raised after the index became ready")
	}
}

func TestSchedulingReportNilWiringIsNoop(t *testing.T) {
	ctx := context.Background()
	raised, err := RaiseSchedulingReport(ctx, nil, nil)
	if err != nil || raised {
		t.Fatalf("nil lister/notifier: raised=%v err=%v, want false/nil", raised, err)
	}
	ready := make(chan struct{})
	close(ready)
	StartSchedulingReport(ctx, ready, nil, nil) // must not panic
}

// notifierFunc adapts a function to SystemNotifier for tests.
type notifierFunc func(SystemNotice)

func (f notifierFunc) Notify(_ context.Context, n SystemNotice) error {
	f(n)
	return nil
}
