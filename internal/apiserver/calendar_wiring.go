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

// calendarTestSeam, when a test sets it, routes runtime calendar sources to a
// loopback test feed. Production leaves it nil, so the public-address guard
// applies to every runtime source.
var calendarTestSeam *calendar.TestSeam

// newCalendarManager builds the runtime calendar manager over the graph's
// registry and poller. It loads the sources saved under dataDir and answers
// removal checks from the automations that lister returns.
func newCalendarManager(reg *calendar.Registry, poller *calendar.Poller, dataDir string, lister service.BrainLister) (*calendar.Manager, error) {
	opts := calendar.ManagerOptions{References: service.NewCalendarReferenceFinder(lister)}
	if calendarTestSeam != nil {
		opts = opts.WithTestSeam(*calendarTestSeam)
	}
	return calendar.NewManager(reg, poller, dataDir, opts)
}
