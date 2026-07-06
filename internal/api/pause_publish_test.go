package api

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestPublishPauseCommand_FiltersToSubscribedRunners confirms the
// helper that fans out pause/resume SSE commands publishes to only the
// runners actually subscribed to the affected project. This is what
// keeps runner-local pauseCache in sync with the API's DB state after
// PWA pause dial changes, without spamming unrelated runners.
func TestPublishPauseCommand_FiltersToSubscribedRunners(t *testing.T) {
	runners := []types.RunnerInfo{
		{RunnerID: "r-orion", Status: types.RunnerStatusOnline, Projects: []string{"orion-ai"}},
		{RunnerID: "r-multi", Status: types.RunnerStatusOnline, Projects: []string{"orion-ai", "brain-api"}},
		{RunnerID: "r-other", Status: types.RunnerStatusOnline, Projects: []string{"other-project"}},
		{RunnerID: "r-offline", Status: types.RunnerStatusOffline, Projects: []string{"orion-ai"}},
	}
	registry := &mockRunnerRegistryService{
		listRunnersFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return &types.RunnerListResponse{Runners: runners, Total: len(runners)}, nil
		},
	}
	fakeHub := newFakePublisher()

	publishPauseCommand(context.Background(), fakeHub, registry, "orion-ai", "tasks", true)

	// Should publish to r-orion and r-multi only. Not r-other (wrong
	// project), not r-offline (not online — those runners won't process
	// commands until they reconnect).
	publishes := fakeHub.published()
	if len(publishes) != 2 {
		t.Fatalf("published to %d runners, want 2 (r-orion, r-multi); got %#v", len(publishes), publishes)
	}
	targets := map[string]bool{}
	for _, p := range publishes {
		targets[p.runnerID] = true
		if p.command != "pause" {
			t.Errorf("command = %q, want pause", p.command)
		}
		payload, ok := p.payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", p.payload)
		}
		if payload["projectId"] != "orion-ai" {
			t.Errorf("payload.projectId = %v, want orion-ai", payload["projectId"])
		}
		if payload["scope"] != "tasks" {
			t.Errorf("payload.scope = %v, want tasks", payload["scope"])
		}
	}
	if !targets["r-orion"] || !targets["r-multi"] {
		t.Errorf("targets = %v, want {r-orion, r-multi}", targets)
	}
	if targets["r-other"] || targets["r-offline"] {
		t.Errorf("unexpected targets: %v", targets)
	}
}

// TestPublishPauseCommand_GlobalPauseHitsAllOnlineRunners confirms
// that when projectID is empty (global pause), every online runner
// receives the command regardless of their project subscriptions.
func TestPublishPauseCommand_GlobalPauseHitsAllOnlineRunners(t *testing.T) {
	runners := []types.RunnerInfo{
		{RunnerID: "r-a", Status: types.RunnerStatusOnline, Projects: []string{"orion-ai"}},
		{RunnerID: "r-b", Status: types.RunnerStatusOnline, Projects: []string{"other-project"}},
		{RunnerID: "r-offline", Status: types.RunnerStatusOffline, Projects: []string{"anywhere"}},
	}
	registry := &mockRunnerRegistryService{
		listRunnersFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return &types.RunnerListResponse{Runners: runners, Total: len(runners)}, nil
		},
	}
	fakeHub := newFakePublisher()

	publishPauseCommand(context.Background(), fakeHub, registry, "", "", true)

	publishes := fakeHub.published()
	if len(publishes) != 2 {
		t.Fatalf("published to %d runners, want 2 (both online runners); got %#v", len(publishes), publishes)
	}
}

// TestPublishPauseCommand_ResumeUsesResumeCommand confirms that
// pause=false publishes "resume" (not "pause").
func TestPublishPauseCommand_ResumeUsesResumeCommand(t *testing.T) {
	registry := &mockRunnerRegistryService{
		listRunnersFunc: func(ctx context.Context) (*types.RunnerListResponse, error) {
			return &types.RunnerListResponse{Runners: []types.RunnerInfo{
				{RunnerID: "r-a", Status: types.RunnerStatusOnline, Projects: []string{"p"}},
			}}, nil
		},
	}
	fakeHub := newFakePublisher()

	publishPauseCommand(context.Background(), fakeHub, registry, "p", "tasks", false)

	publishes := fakeHub.published()
	if len(publishes) != 1 || publishes[0].command != "resume" {
		t.Fatalf("published = %#v, want single resume", publishes)
	}
}

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

type fakePublish struct {
	runnerID string
	command  string
	payload  interface{}
}

type fakePublisher struct {
	mu       chan struct{}
	captured []fakePublish
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{mu: make(chan struct{}, 1)}
}

func (f *fakePublisher) PublishRunnerCommand(runnerID string, command string, payload interface{}) {
	f.mu <- struct{}{}
	f.captured = append(f.captured, fakePublish{runnerID: runnerID, command: command, payload: payload})
	<-f.mu
}

func (f *fakePublisher) published() []fakePublish {
	f.mu <- struct{}{}
	defer func() { <-f.mu }()
	out := make([]fakePublish, len(f.captured))
	copy(out, f.captured)
	return out
}
