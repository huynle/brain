package apiserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/attentionstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
)

// staticSchedulingLister serves one page of automations and no tasks.
type staticSchedulingLister struct{ automations []types.BrainEntry }

func (s staticSchedulingLister) List(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	if req.Type != "automation" || req.Offset > 0 {
		return &types.ListEntriesResponse{}, nil
	}
	return &types.ListEntriesResponse{Entries: s.automations, Total: len(s.automations)}, nil
}

func TestWireSystemNoticesPublishesNotifierAndRaisesReport(t *testing.T) {
	ctx := context.Background()
	store, err := attentionstore.Open(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	attention := service.NewAttentionService(store)
	cfg := config.Config{Attention: config.AttentionConfig{SystemRecipients: []string{"alice"}}}
	lister := staticSchedulingLister{automations: []types.BrainEntry{{
		Path: "projects/canis/automation/a.md", Title: "A", Type: "automation", Status: "active",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 1 * 1"},
	}}}
	ready := make(chan struct{})
	close(ready)

	notifier := wireSystemNotices(ctx, attention, cfg, lister, ready)

	if notifier == nil {
		t.Fatal("wireSystemNotices returned a nil notifier")
	}
	if attention.SystemNotifier() == nil {
		t.Fatal("notifier was not published on the attention service")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		items, err := attention.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice", IncludeSnoozed: true})
		if err != nil {
			t.Fatal(err)
		}
		if hasSchedulingReportItem(items) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("startup scheduling report did not reach the configured recipient")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWireSystemNoticesNilWiringIsNoop(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}

	notifier := wireSystemNotices(ctx, nil, cfg, nil, nil)
	if err := service.NotifySystem(ctx, notifier, service.SystemNotice{Kind: "k", Title: "t"}); err != nil {
		t.Fatalf("notice through nil-wired notifier = %v, want nil", err)
	}
}

func hasSchedulingReportItem(items []types.Attention) bool {
	for _, it := range items {
		if it.Kind == "scheduling_report" {
			return true
		}
	}
	return false
}
