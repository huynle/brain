package apiserver

import (
	"context"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/service"
)

// wireSystemNotices builds the system notifier over the attention service,
// publishes it there for later consumers, and starts the one-shot startup
// scheduling report once the boot index scan has finished (ready). It returns
// the notifier. A nil attention service yields a no-op notifier, and a nil
// lister starts no report, so wiring can never fail startup.
func wireSystemNotices(ctx context.Context, attention *service.AttentionService, cfg config.Config, lister service.SchedulingEntryLister, ready <-chan struct{}) service.SystemNotifier {
	notifier := service.NewSystemNotifier(attention, cfg.Attention.SystemRecipients)
	attention.SetSystemNotifier(notifier)
	service.StartSchedulingReport(ctx, ready, lister, notifier)
	return notifier
}
