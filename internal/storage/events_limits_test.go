package storage

import (
	"context"
	"testing"
)

func TestTenantEventsHistoryLimits(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{a, b} {
		for i := 0; i < 1001; i++ {
			if _, err := s.InsertEvent(ctx, "limit-test", "{}", "", "test"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []*TenantStore{a, b} {
		for _, tc := range []struct{ limit, want int }{{-1, 100}, {0, 100}, {1, 1}, {1000, 1000}, {1001, 1000}} {
			rows, err := s.GetEventsByType(ctx, "limit-test", tc.limit)
			if err != nil || len(rows) != tc.want {
				t.Fatalf("limit %d: got %d, err %v", tc.limit, len(rows), err)
			}
		}
	}
}
