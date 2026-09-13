package apiserver

import (
	"context"
	"sync"

	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/tenant"
)

// startSingleGraphWorkers is boot-only. It preserves the immediate scheduler,
// automation replay and reminder sweep, and existing ticker intervals. Do NOT
// call this from a lazy graph-cache constructor: durable scheduling must operate
// independently of HTTP activity/cache residency. The returned stop cancels and
// joins every loop before the caller closes the graph and finally the DB owner.
// The caller owns startup and invokes this exactly once per single-mode boot.
func startSingleGraphWorkers(parent context.Context, g *tenantGraph) func() {
	ctx, cancel := context.WithCancel(tenant.Into(parent, g.id))
	done := []<-chan struct{}{
		wireSupervisorControlEvents(ctx, g.bridge, g.runners, g.events),
		g.tasks.StartClaimCleanup(ctx, service.DefaultClaimCleanupInterval),
		g.runners.StartLifecycleManager(ctx, service.DefaultLifecycleInterval),
		g.scheduler.Start(ctx, service.DefaultSchedulerInterval),
		g.cascade.Start(ctx),
	}
	var wg sync.WaitGroup
	launch := func(run func()) { wg.Add(1); go func() { defer wg.Done(); run() }() }
	launch(func() { g.automations.Start(ctx, g.eventHub) })
	launch(func() { g.goals.Start(ctx, g.eventHub) })
	launch(func() { g.reminders.Start(ctx) })
	launch(func() { g.webhookDispatcher.Start(ctx) })
	launch(func() { g.triggerDispatcher.Start(ctx) })
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			for _, ch := range done {
				<-ch
			}
			wg.Wait()
		})
	}
}
