package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestGraphLimitBoundariesWithLargeFixture(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	target := saveEntry(t, svc, types.CreateEntryRequest{Type: "plan", Title: "Shared target", Project: "graph-cap"})
	source := saveEntry(t, svc, types.CreateEntryRequest{Type: "plan", Title: "Source", Project: "graph-cap", RelatedEntries: []string{target.ID}})
	for i := 0; i < 105; i++ {
		saveEntry(t, svc, types.CreateEntryRequest{Type: "plan", Title: fmt.Sprintf("Related %d", i), Project: "graph-cap", RelatedEntries: []string{target.ID}})
	}
	// All 106 sources have no incoming links; 105 share the source's target.
	for _, limit := range []int{-1, 0, 1, 99, 100, 101, 1000000} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			wantRelated, wantOrphans := limit, limit
			if limit <= 0 {
				wantRelated, wantOrphans = 10, 50
			}
			if limit > 100 {
				wantRelated, wantOrphans = 100, 100
			}
			related, err := svc.GetRelated(ctx, source.Path, limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(related) != wantRelated {
				t.Errorf("related=%d, want %d", len(related), wantRelated)
			}
			orphans, err := svc.GetOrphans(ctx, "plan", limit, "graph-cap")
			if err != nil {
				t.Fatal(err)
			}
			if len(orphans) != wantOrphans {
				t.Errorf("orphans=%d, want %d", len(orphans), wantOrphans)
			}
		})
	}
}
