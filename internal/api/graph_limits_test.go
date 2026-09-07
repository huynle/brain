package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestGraphLimitBoundaries(t *testing.T) {
	for _, endpoint := range []string{"related", "orphans"} {
		for _, tc := range []struct {
			query string
			want  int
		}{
			{"", 0}, {"garbage", 0}, {"0", 0}, {"-1", 0}, {"999999999999999999999999999999", 0},
			{"1", 1}, {"99", 99}, {"100", 100}, {"101", 100}, {"1000000", 100},
		} {
			t.Run(endpoint+"/"+tc.query, func(t *testing.T) {
				want := tc.want
				if want == 0 && endpoint == "related" {
					want = 10
				}
				calls := 0
				check := func(limit int) {
					calls++
					if limit != want {
						t.Errorf("service limit=%d, want %d", limit, want)
					}
				}
				brain := &mockBrainService{
					getRelatedFunc: func(_ context.Context, _ string, limit int) ([]types.BrainEntry, error) {
						check(limit)
						return []types.BrainEntry{}, nil
					},
					getOrphansFunc: func(_ context.Context, typ string, limit int, project string) ([]types.BrainEntry, error) {
						check(limit)
						if typ != "plan" || project != "p" {
							t.Errorf("scope lost: %s/%s", typ, project)
						}
						return []types.BrainEntry{}, nil
					},
				}
				h := NewHandler(brain)
				r := httptest.NewRequest("GET", "/"+endpoint+"?project=p&type=plan&limit="+tc.query, nil)
				w := httptest.NewRecorder()
				if endpoint == "related" {
					h.HandleGetRelated(w, r)
				} else {
					h.HandleGetOrphans(w, r)
				}
				if w.Code != 200 || calls != 1 {
					t.Fatalf("status=%d calls=%d", w.Code, calls)
				}
			})
		}
	}
}
