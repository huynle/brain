package service

import (
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestBuildAttentionPushPayload_IsCompactAndDeepLinks(t *testing.T) {
	item := &types.Attention{
		ID: "attn_1", Title: "Blocked", Body: "needs a decision",
		Severity: "critical", Kind: "task_blocked", Project: "canis", TaskID: "t1",
	}
	raw, err := buildAttentionPushPayload(item)
	if err != nil {
		t.Fatal(err)
	}
	var got attentionPushPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "attn_1" || got.Title != "Blocked" || got.TaskID != "t1" || got.Severity != "critical" {
		t.Fatalf("payload lost required fields: %+v", got)
	}
}

func TestPushEndpointGone(t *testing.T) {
	for _, tc := range []struct {
		status int
		gone   bool
	}{{404, true}, {410, true}, {201, false}, {429, false}, {500, false}} {
		if got := pushEndpointGone(tc.status); got != tc.gone {
			t.Errorf("pushEndpointGone(%d) = %v, want %v", tc.status, got, tc.gone)
		}
	}
}

func TestShouldDeliverAttention(t *testing.T) {
	if !shouldDeliverAttention(types.Event{Type: types.EventAttentionCreated}) {
		t.Error("attention.created should deliver")
	}
	if shouldDeliverAttention(types.Event{Type: types.EventAttentionUpdated}) {
		t.Error("attention.updated must NOT re-notify")
	}
	if shouldDeliverAttention(types.Event{Type: types.EventTaskBlocked}) {
		t.Error("unrelated events must not deliver")
	}
}
