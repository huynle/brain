package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

type jobAPIStub struct {
	BulkJobService
	calls int
}

func (s *jobAPIStub) Create(_ context.Context, r types.BulkJobRequest, _ string) (*types.BulkJob, error) {
	s.calls++
	return &types.BulkJob{ID: "job", RequestID: r.RequestID, State: "queued"}, nil
}
func (s *jobAPIStub) List(context.Context) ([]types.BulkJob, error) {
	s.calls++
	return []types.BulkJob{}, nil
}

type jobTokenValidator struct{}

func (jobTokenValidator) ValidateToken(_ context.Context, token string) (*storage.Token, error) {
	return &storage.Token{Name: "test", Token: token, Scope: token}, nil
}
func TestBulkJobsAPIAuthAndStrictJSON(t *testing.T) {
	for _, test := range []struct {
		scope, body string
		want        int
	}{
		{"admin:*", `{"request_id":"request-01","operation":"delete","paths":["a"]}`, 202},
		{"read:*", `{"request_id":"request-01","operation":"delete","paths":["a"]}`, 403},
		{"runner:*", `{"request_id":"request-01","operation":"delete","paths":["a"]}`, 403},
		{"admin:*", `{"request_id":"request-01","tenant_id":"other"}`, 400},
		{"admin:*", `{"filters":[{"project":"p","unknown":"x"}]}`, 400},
		{"admin:*", `{} {}`, 400},
	} {
		stub := &jobAPIStub{}
		cfg := testConfig()
		cfg.EnableAuth = true
		router := NewRouter(cfg, WithHandler(NewHandler(nil, WithBulkJobService(stub))), WithTokenValidator(jobTokenValidator{}))
		req := httptest.NewRequest("POST", "/api/v1/bulk-jobs", strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer "+test.scope)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatalf("%s %s: %d %s", test.scope, test.body, w.Code, w.Body.String())
		}
		if w.Code != 202 && stub.calls != 0 {
			t.Fatal("invalid request reached service")
		}
	}
	for _, scope := range []string{"read:*", "runner:*"} {
		stub := &jobAPIStub{}
		cfg := testConfig()
		cfg.EnableAuth = true
		router := NewRouter(cfg, WithHandler(NewHandler(nil, WithBulkJobService(stub))), WithTokenValidator(jobTokenValidator{}))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/bulk-jobs", nil)
		req.Header.Set("Authorization", "Bearer "+scope)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 403 || stub.calls != 0 {
			t.Fatalf("job audit exposed to %s: %d", scope, w.Code)
		}
	}
}
