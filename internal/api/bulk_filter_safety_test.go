package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestBulkFilterSafety(t *testing.T) {
	filters := []string{`{}`, `{"feature_id":""}`, `{"project":""}`, `{"type":""}`, `{"status":""}`, `{"priority":""}`, `{"tags":["", " \t", ", ,"]}`, `{"feature_id":"","project":"","type":"","status":"","priority":"","tags":[""]}`}
	for _, op := range []string{"delete", "update"} {
		for _, filter := range filters {
			for _, mode := range []string{``, `,"dry_run":true`, `,"force":true`, `,"dry_run":true,"force":true`, `query-force`} {
				t.Run(op+filter+mode, func(t *testing.T) {
					calls := 0
					brain := &mockBrainService{
						bulkDeleteFunc: func(context.Context, types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
							calls++
							return &types.BulkDeleteResponse{}, nil
						},
						bulkUpdateFunc: func(context.Context, types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
							calls++
							return &types.BulkUpdateResponse{}, nil
						},
					}
					extra := ""
					if op == "update" {
						extra = `,"updates":{"status":"pending"}`
					}
					query, bodyMode := "", mode
					if mode == "query-force" {
						query, bodyMode = "?force=true", ""
					}
					r := httptest.NewRequest(http.MethodPost, "/entries/bulk-"+op+query, strings.NewReader(`{"filter":`+filter+extra+bodyMode+`}`))
					w := httptest.NewRecorder()
					h := NewHandler(brain)
					if op == "update" {
						h.HandleBulkUpdate(w, r)
					} else {
						h.HandleBulkDelete(w, r)
					}
					if w.Code != 422 || calls != 0 {
						t.Fatalf("status=%d service calls=%d, want 422 and 0; %s", w.Code, calls, w.Body.String())
					}
					var body types.ErrorResponse
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if body.Error != "Validation Error" || len(body.Details) != 1 || body.Details[0].Field != "filter" {
						t.Fatalf("unexpected validation: %+v", body)
					}
				})
			}
		}
	}
}

func TestBulkFilterSafetyPreservesEffectiveFilters(t *testing.T) {
	for _, field := range []string{"feature_id", "project", "type", "status", "priority", "generated_by", "generated_key", "agent", "executor", "execution_mode", "tags"} {
		value := `"x"`
		if field == "tags" {
			value = `[" , ", " x "]`
		}
		if field == "generated_by" || field == "generated_key" || field == "agent" || field == "executor" || field == "execution_mode" {
			value = `""`
		}
		for _, op := range []string{"delete", "update"} {
			t.Run(op+field, func(t *testing.T) {
				calls := 0
				brain := &mockBrainService{
					bulkDeleteFunc: func(context.Context, types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
						calls++
						return &types.BulkDeleteResponse{}, nil
					},
					bulkUpdateFunc: func(context.Context, types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
						calls++
						return &types.BulkUpdateResponse{}, nil
					},
				}
				extra := ""
				if op == "update" {
					extra = `,"updates":{"status":"pending"}`
				}
				r := httptest.NewRequest("POST", "/", strings.NewReader(fmt.Sprintf(`{"filter":{%q:%s},"dry_run":true%s}`, field, value, extra)))
				w := httptest.NewRecorder()
				h := NewHandler(brain)
				if op == "update" {
					h.HandleBulkUpdate(w, r)
				} else {
					h.HandleBulkDelete(w, r)
				}
				if w.Code != 200 || calls != 1 {
					t.Fatalf("status=%d calls=%d: %s", w.Code, calls, w.Body.String())
				}
			})
		}
	}
}
