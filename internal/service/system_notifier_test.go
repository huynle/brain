package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func systemNoticeFixture(dedup string) SystemNotice {
	return SystemNotice{
		Kind:       "scheduling_report",
		Severity:   types.AttentionSeverityWarning,
		Title:      "Scheduling semantics changed",
		Body:       "- automation a1: schedule changed",
		SourceType: "system",
		DedupKey:   dedup,
	}
}

func attentionRecipientsOf(t *testing.T, svc *AttentionService, ctx context.Context, recipient string) []types.Attention {
	t.Helper()
	items, err := svc.ListAttention(ctx, types.AttentionListFilter{Recipient: recipient, IncludeSnoozed: true})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestSystemNotifierRecipientResolution(t *testing.T) {
	t.Run("configured recipients win over existing inbox owners", func(t *testing.T) {
		svc, ctx, _ := newAttentionService(t)
		if _, err := svc.CreateAttention(ctx, "carol", types.CreateAttentionRequest{Kind: "k", Title: "t"}); err != nil {
			t.Fatal(err)
		}
		n := &systemNotifier{attention: svc, configured: []string{"alice", "bob"}}
		got, err := n.resolveSystemRecipients(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"alice", "bob"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("recipients = %v, want configured %v", got, want)
		}
	})

	t.Run("falls back to recipients with attention items", func(t *testing.T) {
		svc, ctx, _ := newAttentionService(t)
		for _, r := range []string{"carol", "alice"} {
			if _, err := svc.CreateAttention(ctx, r, types.CreateAttentionRequest{Kind: "k", Title: "t"}); err != nil {
				t.Fatal(err)
			}
		}
		n := &systemNotifier{attention: svc}
		got, err := n.resolveSystemRecipients(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"alice", "carol"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("recipients = %v, want fallback %v", got, want)
		}
	})

	t.Run("no configured recipients and an empty inbox resolves to none", func(t *testing.T) {
		svc, ctx, _ := newAttentionService(t)
		n := &systemNotifier{attention: svc}
		got, err := n.resolveSystemRecipients(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("recipients = %v, want none", got)
		}
	})
}

func TestSystemNotifierNotifyFansOutOneItemPerRecipient(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	n := NewSystemNotifier(svc, []string{"alice", "bob"})
	if err := n.Notify(ctx, systemNoticeFixture("scheduling-report:abc")); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	for _, r := range []string{"alice", "bob"} {
		items := attentionRecipientsOf(t, svc, ctx, r)
		if len(items) != 1 {
			t.Fatalf("%s has %d items, want 1", r, len(items))
		}
		got := items[0]
		if got.Kind != "scheduling_report" || got.Severity != types.AttentionSeverityWarning ||
			got.DedupKey != "scheduling-report:abc" || got.SourceType != "system" {
			t.Fatalf("%s item = %+v", r, got)
		}
	}
}

func TestSystemNotifierWithNoRecipientsRaisesNothing(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	n := NewSystemNotifier(svc, nil)
	if err := n.Notify(ctx, systemNoticeFixture("scheduling-report:abc")); err != nil {
		t.Fatalf("Notify with no recipients should log and return nil, got %v", err)
	}
	if items, err := svc.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice", IncludeSnoozed: true}); err != nil || len(items) != 0 {
		t.Fatalf("expected no items, got %d (err %v)", len(items), err)
	}
}

func TestSystemNotifierRejectsNoticeWithoutKindOrTitle(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	n := NewSystemNotifier(svc, []string{"alice"})
	if err := n.Notify(ctx, SystemNotice{Title: "no kind"}); err == nil {
		t.Fatal("notice without kind should be rejected")
	}
	if err := n.Notify(ctx, SystemNotice{Kind: "k"}); err == nil {
		t.Fatal("notice without title should be rejected")
	}
}

func TestSystemNotifierNilSafety(t *testing.T) {
	ctx := context.Background()

	if err := NotifySystem(ctx, nil, systemNoticeFixture("x")); err != nil {
		t.Fatalf("NotifySystem with nil notifier = %v, want nil", err)
	}

	var typedNil *systemNotifier
	if err := typedNil.Notify(ctx, systemNoticeFixture("x")); err != nil {
		t.Fatalf("nil *systemNotifier Notify = %v, want nil", err)
	}
	if err := NotifySystem(ctx, typedNil, systemNoticeFixture("x")); err != nil {
		t.Fatalf("NotifySystem with typed-nil notifier = %v, want nil", err)
	}

	var svc *AttentionService
	if got := svc.SystemNotifier(); got != nil {
		t.Fatalf("nil AttentionService SystemNotifier() = %v, want nil", got)
	}
	svc.SetSystemNotifier(NewSystemNotifier(nil, nil)) // must not panic
}

func TestAttentionServiceSystemNotifierHolder(t *testing.T) {
	svc, _, _ := newAttentionService(t)
	if got := svc.SystemNotifier(); got != nil {
		t.Fatalf("unset SystemNotifier() = %v, want nil", got)
	}
	n := NewSystemNotifier(svc, []string{"alice"})
	svc.SetSystemNotifier(n)
	if got := svc.SystemNotifier(); got != n {
		t.Fatalf("SystemNotifier() = %v, want the notifier that was set", got)
	}
}
