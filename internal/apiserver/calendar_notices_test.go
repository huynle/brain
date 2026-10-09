package apiserver

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/service"
)

// recordingSystemNotifier captures the notices the calendar poller raises.
type recordingSystemNotifier struct {
	got []service.SystemNotice
	err error
}

func (r *recordingSystemNotifier) Notify(_ context.Context, n service.SystemNotice) error {
	r.got = append(r.got, n)
	return r.err
}

// The calendar poller raises notices in its own type, because the calendar
// package cannot import service. The adapter must carry every field across.
func TestCalendarNoticesCarryEveryFieldToTheSystemNotifier(t *testing.T) {
	rec := &recordingSystemNotifier{}
	notify := calendarNotices(rec)
	notice := calendar.Notice{
		Kind: "calendar_stale", Severity: "warning", Title: "Calendar team is stale",
		Body: "No successful refresh since 2026-10-08T12:00:00Z.", Project: "",
		SourceType: "calendar", DedupKey: "calendar-stale:team:2026-10-08T12:00:00Z",
	}
	if err := notify(context.Background(), notice); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("notices delivered: %d want 1", len(rec.got))
	}
	want := service.SystemNotice{
		Kind: notice.Kind, Severity: notice.Severity, Title: notice.Title, Body: notice.Body,
		Project: notice.Project, SourceType: notice.SourceType, DedupKey: notice.DedupKey,
	}
	if rec.got[0] != want {
		t.Fatalf("adapted notice:\n got %+v\nwant %+v", rec.got[0], want)
	}
}

// Delivery errors reach the poller, which keeps the episode open for a retry.
func TestCalendarNoticesReturnDeliveryErrors(t *testing.T) {
	rec := &recordingSystemNotifier{err: context.DeadlineExceeded}
	if err := calendarNotices(rec)(context.Background(), calendar.Notice{Kind: "calendar_stale", Title: "t"}); err == nil {
		t.Fatal("delivery error was swallowed; the poller would not retry")
	}
}

// Before the attention service is wired the notifier is nil and must not
// panic or fail: the poller treats an error as "retry later".
func TestCalendarNoticesWithNilNotifierIsNoOp(t *testing.T) {
	if err := calendarNotices(nil)(context.Background(), calendar.Notice{Kind: "calendar_stale", Title: "t"}); err != nil {
		t.Fatalf("nil notifier returned %v; want nil no-op", err)
	}
}
