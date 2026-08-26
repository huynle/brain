package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// FeatureRunner is the narrow interface FeatureCascadeService needs from the
// scheduler. It's satisfied by *SchedulerService and trivial to fake in tests.
type FeatureRunner interface {
	RunFeatureNow(ctx context.Context, projectID, featureID string, force bool) (*types.RunFeatureResponse, error)
}

// dependentChainSweeper advances standing run-with-dependents requests.
//
// Separate from FeatureRunner because the two answer different questions:
// FeatureRunner drains ONE feature that is already running, while the sweeper
// asks whether a NEW feature in a chain has become eligible. The latter cannot
// be event-keyed on the feature itself — a gated feature has no running tasks,
// so no event ever carries its id, and a marker for it would sit in a map
// nothing consults. Optional: nil disables chains without affecting the
// single-feature cascade.
type dependentChainSweeper interface {
	SweepDependentChains(ctx context.Context, projectID string) int
}

// featureCascadeKey uniquely identifies a feature-cascade marker.
type featureCascadeKey struct {
	projectID string
	featureID string
}

// FeatureCascadeService tracks which features the user has manually triggered
// via RunFeatureNow and, when each in-flight task in those features completes,
// asks the scheduler to dispatch the next ready task in the same feature.
//
// This is how "dispatch what fits, queue the rest" behaves end-to-end even
// while the project is paused: pause halts the normal scheduler tick, but the
// cascade keeps feeding queued tasks one-by-one as slots free.
//
// State is in-memory only. A server restart clears all cascade markers; users
// would need to re-trigger the feature. This is intentional for v1 — the
// alternative (a persistence table + cleanup logic) is significant complexity
// for a transient signal.
type FeatureCascadeService struct {
	hub     *realtime.EventHub
	runner  FeatureRunner
	sweeper dependentChainSweeper

	mu       sync.RWMutex
	cascades map[featureCascadeKey]struct{}

	// running indicates Start() is active. Guards against double-start.
	running bool
}

// NewFeatureCascadeService constructs a cascade service. The hub may be nil
// (e.g. in tests that only exercise Register/IsActive); in that case Start
// will be a no-op.
func NewFeatureCascadeService(hub *realtime.EventHub, runner FeatureRunner) *FeatureCascadeService {
	s := &FeatureCascadeService{
		hub:      hub,
		runner:   runner,
		cascades: make(map[featureCascadeKey]struct{}),
	}
	// The runner is usually the scheduler, which is also the sweeper.
	if sw, ok := runner.(dependentChainSweeper); ok {
		s.sweeper = sw
	}
	return s
}

// chainSweepInterval backstops the event path.
//
// Several gate-opening transitions emit no event this service subscribes to:
// archiving the last task of a feature, deleting a task, and editing
// feature_depends_on. A chain waiting on any of those would stall until the
// next unrelated completion. The ticker makes progress independent of events
// arriving at all.
const chainSweepInterval = 30 * time.Second

// Register marks a feature as in manual-cascade mode. Idempotent.
func (s *FeatureCascadeService) Register(projectID, featureID string) {
	if projectID == "" || featureID == "" {
		return
	}
	s.mu.Lock()
	s.cascades[featureCascadeKey{projectID: projectID, featureID: featureID}] = struct{}{}
	s.mu.Unlock()
}

// Unregister clears the cascade marker for a feature. Safe to call when
// not registered.
func (s *FeatureCascadeService) Unregister(projectID, featureID string) {
	s.mu.Lock()
	delete(s.cascades, featureCascadeKey{projectID: projectID, featureID: featureID})
	s.mu.Unlock()
}

// IsActive returns whether a feature is currently being cascaded.
func (s *FeatureCascadeService) IsActive(projectID, featureID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.cascades[featureCascadeKey{projectID: projectID, featureID: featureID}]
	return ok
}

// Start subscribes to the event hub and runs the cascade loop until ctx
// is canceled. Safe to call once; subsequent calls are no-ops.
func (s *FeatureCascadeService) Start(ctx context.Context) {
	if s.hub == nil || s.runner == nil {
		slog.Info("feature cascade not started: hub or runner missing")
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	// We want every task lifecycle terminal event. Filter by type patterns
	// so the channel doesn't get every event in the system.
	ch, unsub := s.hub.Subscribe(realtime.EventFilter{
		TypePatterns: []string{
			types.EventTaskCompleted,
			types.EventTaskFailed,
			types.EventTaskCancelled,
			// A completed feature is what opens a dependent's gate.
			types.EventFeatureCompleted,
		},
	})

	go func() {
		defer func() {
			unsub()
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			slog.Info("feature cascade stopped")
		}()
		slog.Info("feature cascade started")
		ticker := time.NewTicker(chainSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				s.handleEvent(ctx, evt)
				// Any terminal task or completed feature may have opened
				// a gate somewhere in a standing chain.
				s.sweepChains(ctx, evt.ProjectID)
			case <-ticker.C:
				s.sweepChains(ctx, "")
			}
		}
	}()
}

// handleEvent fires the next dispatch pass for a feature when one of its
// tasks reaches a terminal state. Lookup is O(1) on the cascade map.
//
// Three exit paths:
//  1. Event has no feature_id or the feature isn't registered: ignore.
//  2. RunFeatureNow dispatches at least one new task or has queued tasks:
//     leave the cascade active.
//  3. RunFeatureNow reports no_ready_tasks: feature has drained (either
//     fully completed or remaining tasks are blocked); unregister to stop
//     reacting to subsequent events for this feature.
func (s *FeatureCascadeService) handleEvent(ctx context.Context, evt types.Event) {
	if evt.ProjectID == "" || evt.FeatureID == "" {
		return
	}
	if !s.IsActive(evt.ProjectID, evt.FeatureID) {
		return
	}

	resp, err := s.runner.RunFeatureNow(ctx, evt.ProjectID, evt.FeatureID, true)
	if err != nil {
		slog.Warn("feature cascade dispatch failed",
			"project_id", evt.ProjectID,
			"feature_id", evt.FeatureID,
			"trigger_event", evt.Type,
			"trigger_task", evt.TaskID,
			"error", err,
		)
		return
	}
	if resp == nil {
		return
	}

	slog.Info("feature cascade tick",
		"project_id", evt.ProjectID,
		"feature_id", evt.FeatureID,
		"trigger_event", evt.Type,
		"trigger_task", evt.TaskID,
		"dispatched", resp.DispatchedCount,
		"queued", len(resp.Queued),
		"reason", resp.Reason,
	)

	// Drain detection.
	//
	// This used to key on resp.Reason == "no_ready_tasks", which is NOT a
	// drain signal: it means only that nothing was READY at this instant.
	// A feature with a fan-in — two tasks running, a third waiting on both
	// — reports exactly that the moment the FIRST of the two completes,
	// while it is still mid-flight. The cascade unregistered there, the
	// second completion then found no cascade, and the third task was
	// never dispatched. Under a paused project nothing else would pick it
	// up, so the tail of the feature was silently dropped. Reproduced
	// live before this fix: fan-in task stranded at ready indefinitely,
	// then dispatched within a second of resuming the project.
	//
	// Outstanding counts tasks that can still produce work (pending or
	// in_progress), so 0 is an unambiguous "nothing more can come from
	// this feature". nil means the server could not measure it.
	switch {
	case resp.Outstanding != nil && *resp.Outstanding == 0:
		s.Unregister(evt.ProjectID, evt.FeatureID)
	case resp.Outstanding == nil && resp.Reason == "no_ready_tasks":
		// Legacy fallback for a runner implementation without the
		// feature-task lister wired. Same behaviour as before this fix,
		// including its bug — but only where we genuinely cannot
		// measure, never as a zero-value default.
		s.Unregister(evt.ProjectID, evt.FeatureID)
	}
}

// sweepChains advances standing dependent chains.
//
// projectID empty means "every project with a stored root", which is what the
// ticker asks for; the storage layer treats an empty project as unfiltered.
func (s *FeatureCascadeService) sweepChains(ctx context.Context, projectID string) {
	if s.sweeper == nil {
		return
	}
	if n := s.sweeper.SweepDependentChains(ctx, projectID); n > 0 {
		slog.Info("dependent chains advanced", "project_id", projectID, "dispatched", n)
	}
}
