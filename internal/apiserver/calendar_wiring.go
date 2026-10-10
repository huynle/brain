package apiserver

import (
	"context"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/service"
)

// calendarNotices adapts the system notifier to the calendar poller's sink.
// The poller cannot import service, so the field-for-field mapping lives here.
// A nil notifier is a no-op (service.NotifySystem), and a delivery error is
// returned so the poller retries the episode on its next poll.
func calendarNotices(n service.SystemNotifier) calendar.Notifier {
	return func(ctx context.Context, notice calendar.Notice) error {
		return service.NotifySystem(ctx, n, service.SystemNotice{
			Kind:       notice.Kind,
			Severity:   notice.Severity,
			Title:      notice.Title,
			Body:       notice.Body,
			Project:    notice.Project,
			SourceType: notice.SourceType,
			DedupKey:   notice.DedupKey,
		})
	}
}
