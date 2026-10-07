package brain_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestTaskActionRoutes(t *testing.T) {
	ctx := context.Background()
	opts := brain.RequestOptions{RequestID: "action-fixture", IdempotencyKey: "no-retry"}
	yes, no := true, false
	intent := "assign"
	tests := []struct {
		method, path, body, response string
		call                         func(*brain.Client) error
	}{
		{"POST", "/tasks/p%20q/t%20x/resume", `{"force":true}`, `{"task_id":"t","resumed":false,"reason":"live claim"}`, func(c *brain.Client) error {
			v, e := c.Tasks().Resume(ctx, "p q", "t x", brain.ResumeTaskOptions{Force: &yes}, opts)
			if e == nil && (v.Resumed || v.Reason == nil || *v.Reason != "live claim") {
				t.Error("lost no-op")
			}
			return e
		}},
		{"POST", "/tasks/p%20q/t%20x/resume-with-context", `{"injected_context":"context","prefer_same_session":false}`, `{"task_id":"t","resumed":true,"resume_mode":"rehydrate"}`, func(c *brain.Client) error {
			v, e := c.Tasks().ResumeWithContext(ctx, "p q", "t x", brain.ResumeWithContextOptions{InjectedContext: "context", PreferSameSession: &no}, opts)
			if e == nil && (!v.Resumed || v.ResumeMode != "rehydrate") {
				t.Error("lost embedded fields")
			}
			return e
		}},
		{"PUT", "/tasks/p%20q/t%20x/assignment", `{"runner_id":"r","intent":"assign"}`, `{"project_id":"p","task_id":"t","scope":"task","source":"manual","status":"assigned"}`, func(c *brain.Client) error {
			_, e := c.Tasks().Assign(ctx, "p q", "t x", brain.TaskAssignmentRequest{RunnerId: "r", Intent: &intent}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/t%20x/assignment/clear", `{"intent":"clear"}`, `{}`, func(c *brain.Client) error {
			_, e := c.Tasks().ClearAssignment(ctx, "p q", "t x", brain.ClearFeatureAssignmentRequest{Intent: "clear"}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/t%20x/trigger", "", `{"success":true,"triggered":false,"taskId":"t","reason":"already pending"}`, func(c *brain.Client) error {
			v, e := c.Tasks().Trigger(ctx, "p q", "t x", opts)
			if e == nil && (!v.Success || v.Triggered) {
				t.Error("lost trigger result")
			}
			return e
		}},
		{"POST", "/tasks/p%20q/t%20x/run", `{"force":true}`, `{"dispatched":false,"taskId":"t","projectId":"p","reason":"no_online_runner"}`, func(c *brain.Client) error {
			v, e := c.Tasks().Run(ctx, "p q", "t x", brain.RunTaskRequest{Force: &yes}, opts)
			if e == nil && (v.Dispatched || v.Reason == nil) {
				t.Error("lost refusal")
			}
			return e
		}},
		{"POST", "/tasks/p%20q/t%20x/dispatch", `{"targetRunnerId":"r"}`, `{"success":true,"runnerId":"r","leaseId":"l","expiresAt":"later"}`, func(c *brain.Client) error {
			v, e := c.Tasks().Dispatch(ctx, "p q", "t x", brain.DispatchRequest{TargetRunnerId: "r"}, opts)
			if e == nil && v.LeaseId != "l" {
				t.Error("lost lease")
			}
			return e
		}},
		{"GET", "/tasks/p%20q/t%20x/logs?limit=2&offset=0", "", `{"lines":[],"offset":0,"total":3,"limit":2}`, func(c *brain.Client) error {
			v, e := c.Tasks().Logs(ctx, "p q", "t x", url.Values{"limit": {"2"}, "offset": {"0"}})
			if e == nil && (v.Lines == nil || v.Total != 3) {
				t.Error("lost log window")
			}
			return e
		}},
		{"GET", "/projects/p%20q/placement", "", `null`, func(c *brain.Client) error {
			v, e := c.Projects().GetPlacement(ctx, "p q")
			if e == nil && v != nil {
				t.Error("absent policy not null")
			}
			return e
		}},
		{"PUT", "/projects/p%20q/placement", `{"project_id":"body","affinity":"soft"}`, `{"project_id":"p","affinity":"soft"}`, func(c *brain.Client) error {
			_, e := c.Projects().SetPlacement(ctx, "p q", brain.ProjectPlacement{ProjectId: "body", Affinity: "soft"}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/run", `{}`, `{"projectId":"p","featuresConsidered":1,"featuresDispatched":0,"featuresSkipped":1,"totalTasksDispatched":0}`, func(c *brain.Client) error {
			_, e := c.Projects().Run(ctx, "p q", brain.RunProjectRequest{}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/features/f%20x/resume", `{}`, `{"feature_id":"f","total_resumed":0,"total_skipped":1,"results":null,"truncated":true,"total_results":201}`, func(c *brain.Client) error {
			v, e := c.Features().Resume(ctx, "p q", "f x", brain.ResumeTaskOptions{}, opts)
			if e == nil && (v.Results != nil || v.Truncated == nil || !*v.Truncated) {
				t.Error("lost truncation/null")
			}
			return e
		}},
		{"POST", "/tasks/p%20q/features/f%20x/resume-with-context", `{"injected_context":"context"}`, `{"feature_id":"f","total_resumed":1,"total_skipped":0,"results":[{"task_id":"t","resumed":true,"resume_mode":"live_injected","injected_live":true}]}`, func(c *brain.Client) error {
			v, e := c.Features().ResumeWithContext(ctx, "p q", "f x", brain.ResumeWithContextOptions{InjectedContext: "context"}, opts)
			if e == nil && (v.Results == nil || (*v.Results)[0].ResumeMode != "live_injected") {
				t.Error("lost live result")
			}
			return e
		}},
		{"PUT", "/tasks/p%20q/features/f%20x/assignment", `{"runner_id":"r"}`, `{}`, func(c *brain.Client) error {
			_, e := c.Features().Assign(ctx, "p q", "f x", brain.FeatureAssignmentRequest{RunnerId: "r"}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/features/f%20x/assignment/clear", `{"intent":"clear"}`, `{}`, func(c *brain.Client) error {
			_, e := c.Features().ClearAssignment(ctx, "p q", "f x", brain.ClearFeatureAssignmentRequest{Intent: "clear"}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/features/f%20x/checkout", `{}`, `{"created":false,"generatedKey":"key"}`, func(c *brain.Client) error {
			_, e := c.Features().Checkout(ctx, "p q", "f x", brain.FeatureCheckoutOptions{}, opts)
			return e
		}},
		{"POST", "/tasks/p%20q/features/f%20x/run", `{"includeDependents":true}`, `{"dispatched":false,"projectId":"p","featureId":"f","dispatchedCount":0,"skippedCount":1}`, func(c *brain.Client) error {
			_, e := c.Features().Run(ctx, "p q", "f x", brain.RunFeatureRequest{IncludeDependents: &yes}, opts)
			return e
		}},
		{"DELETE", "/tasks/p%20q/features/f%20x/run", "", `{"success":true,"cancelled":false,"projectId":"p","rootFeatureId":"f","detail":"nothing queued"}`, func(c *brain.Client) error {
			v, e := c.Features().Cancel(ctx, "p q", "f x", opts)
			if e == nil && v.Cancelled {
				t.Error("false cancellation")
			}
			return e
		}},
		{"GET", "/tasks/p%20q/chains", "", `{"chains":[]}`, func(c *brain.Client) error {
			v, e := c.Features().Chains(ctx, "p q")
			if e == nil && v.Chains == nil {
				t.Error("lost empty list")
			}
			return e
		}},
	}
	for _, tt := range tests {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			calls := 0
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tt.method || r.URL.RequestURI() != "/api/v1"+tt.path {
					t.Errorf("route %s %s", r.Method, r.URL.RequestURI())
				}
				if tt.method != "GET" && (r.Header.Get("X-Request-ID") != "action-fixture" || r.Header.Get("Idempotency-Key") != "no-retry") {
					t.Error("missing action options")
				}
				if tt.body != "" {
					var got, want any
					if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
						t.Error(e)
					}
					_ = json.Unmarshal([]byte(tt.body), &want)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body=%v want=%v", got, want)
					}
				}
				_, _ = w.Write([]byte(tt.response))
			}, brain.Config{})
			if e := tt.call(c); e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Errorf("requests=%d", calls)
			}
		})
	}
}
