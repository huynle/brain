package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestBulkFilterSafetyBeforeAccess(t *testing.T) {
	for _, raw := range []string{`{}`, `{"feature_id":""}`, `{"project":""}`, `{"type":""}`, `{"status":""}`, `{"priority":""}`, `{"tags":["", " \t", ", ,"]}`} {
		for _, op := range []string{"delete", "update"} {
			for _, dry := range []bool{false, true} {
				for _, force := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/dry=%t/force=%t", op, raw, dry, force), func(t *testing.T) {
						// No dependencies: listing returns a tenant error (or panics),
						// rather than the filter validation error required below.
						defer func() {
							if p := recover(); p != nil {
								t.Errorf("accessed dependencies before rejecting filter: %v", p)
							}
						}()
						var filter types.BulkUpdateFilter
						if err := json.Unmarshal([]byte(raw), &filter); err != nil {
							t.Fatal(err)
						}
						svc := &BrainServiceImpl{}
						var err error
						if op == "update" {
							_, err = svc.BulkUpdate(context.Background(), types.BulkUpdateRequest{Filter: &filter, Updates: &types.UpdateEntryRequest{}, DryRun: dry, Force: force})
						} else {
							_, err = svc.BulkDelete(context.Background(), types.BulkDeleteRequest{Filter: &filter, DryRun: dry, Force: force})
						}
						if err == nil || !strings.Contains(err.Error(), "filter must constrain") {
							t.Fatalf("want ineffective filter error, got %v", err)
						}
					})
				}
			}
		}
	}
}

// Regression coverage for effective empty postfilters and project-wide scope.
// These were valid before the guard and must stay valid, in both bulk modes.
func TestBulkFilterSafetyPreservesServiceMatches(t *testing.T) {
	for _, field := range []string{"generated_by", "generated_key", "agent", "executor", "execution_mode", "project"} {
		for _, op := range []string{"delete", "update"} {
			for _, dry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", op, field, dry), func(t *testing.T) {
					svc, _, _ := newTestBrainService(t)
					ctx := context.Background()
					match := saveEntry(t, svc, types.CreateEntryRequest{Type: "task", Title: "Match", Project: "selected", Status: "pending"})
					otherReq := types.CreateEntryRequest{Type: "task", Title: "Keep", Project: "other", Status: "pending", GeneratedBy: "generator", GeneratedKey: "key", Agent: "build", Executor: "pi", ExecutionMode: "worktree"}
					other := saveEntry(t, svc, otherReq)
					value := ""
					if field == "project" {
						value = "selected"
					}
					var filter types.BulkUpdateFilter
					if err := json.Unmarshal([]byte(fmt.Sprintf(`{%q:%q}`, field, value)), &filter); err != nil {
						t.Fatal(err)
					}
					var results []types.BulkUpdateResult
					status := "completed"
					if op == "update" {
						resp, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{Filter: &filter, Updates: &types.UpdateEntryRequest{Status: &status}, DryRun: dry})
						if err != nil {
							t.Fatal(err)
						}
						results = resp.Results
					} else {
						resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{Filter: &filter, DryRun: dry})
						if err != nil {
							t.Fatal(err)
						}
						results = resp.Results
					}
					if len(results) != 1 || results[0].Path != match.Path || results[0].Status != "ok" {
						t.Fatalf("wrong matches: %+v", results)
					}
					kept, err := svc.Recall(ctx, other.Path)
					if err != nil || kept.Status != "pending" {
						t.Fatalf("nonmatch changed: %+v, %v", kept, err)
					}
					if dry {
						kept, err = svc.Recall(ctx, match.Path)
						if err != nil || kept.Status != "pending" {
							t.Fatalf("dry run mutated match: %+v, %v", kept, err)
						}
					}
				})
			}
		}
	}
}
