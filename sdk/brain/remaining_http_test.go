package brain_test

import (
	"context"
	"encoding/json"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestRemainingHTTPRoutes(t *testing.T) {
	ctx := context.Background()
	title, revision := "new", "rev1"
	off := false
	cases := []struct {
		method, path, body, response string
		call                         func(*brain.Client) error
	}{
		{"PATCH", "/entries/e%20x/metadata", `{"title":"new","expected_revision":"rev1","resume_requested":false}`, `{"id":"e","title":"new"}`, func(c *brain.Client) error {
			_, e := c.Entries().UpdateMetadata(ctx, "e x", brain.MetadataUpdateRequest{Title: &title, ExpectedRevision: &revision, ResumeRequested: &off}, brain.RequestOptions{})
			return e
		}},
		{"POST", "/inject", `{"query":"q"}`, `{"context":"text","entries":null,"total":0}`, func(c *brain.Client) error {
			v, e := c.Inject(ctx, brain.InjectRequest{Query: "q"})
			if e == nil && (v.Context != "text" || v.Entries != nil) {
				t.Error("inject decoding")
			}
			return e
		}},
		{"DELETE", "/tasks/p%20q?confirm=explicit&force=false", "", `{"project":"p","deleted":1,"failed":1,"directory_removed":false,"errors":["failure"]}`, func(c *brain.Client) error {
			v, e := c.Projects().Delete(ctx, "p q", "explicit", false, brain.RequestOptions{})
			if e == nil && (v.Failed != 1 || v.Errors == nil) {
				t.Error("partial deletion lost")
			}
			return e
		}},
		{"GET", "/tasks/p/t/delivery", "", `{"task_id":"t","implementation_status":"pending","delivery":null,"unmet":null}`, func(c *brain.Client) error {
			v, e := c.Tasks().Delivery(ctx, "p", "t")
			if e == nil && (v.TaskId != "t" || v.Delivery != nil || v.Unmet != nil) {
				t.Error("delivery null")
			}
			return e
		}},
		{"POST", "/tasks/p/t/delivery", `{"action":"verify","expected_revision":2}`, `{"delivery":{"revision":3,"required":"merged","repository":"o/r","pull_request":1,"head":"h","target":"main","required_checks":null,"verification_error":"unavailable"},"unmet":["delivery_evidence"]}`, func(c *brain.Client) error {
			v, e := c.Tasks().VerifyDelivery(ctx, "p", "t", brain.DeliveryCommand{Action: brain.Verify, ExpectedRevision: 2}, brain.RequestOptions{})
			if e == nil && (v.Delivery == nil || v.Delivery.VerificationError == nil) {
				t.Error("provider failure lost")
			}
			return e
		}},
		{"GET", "/events/recent?project_id=p&source=api&type=task.%2A", "", `{"events":null,"count":0,"coverage":{"buffered":10,"capacity":100}}`, func(c *brain.Client) error {
			v, e := c.Events().Recent(ctx, url.Values{"project_id": {"p"}, "source": {"api"}, "type": {"task.*"}})
			if e == nil && (v.Events != nil || v.Coverage.Buffered != 10) {
				t.Error("coverage")
			}
			return e
		}},
		{"GET", "/events/wait?after=opaque%2Bcursor&project_id=p&timeout_ms=0", "", `{"events":[],"next_cursor":"next","cursor_expired":true,"timed_out":false,"shutdown":false,"truncated":true}`, func(c *brain.Client) error {
			v, e := c.Events().Wait(ctx, url.Values{"project_id": {"p"}, "timeout_ms": {"0"}, "after": {"opaque+cursor"}})
			if e == nil && (!v.CursorExpired || !v.Truncated || v.NextCursor != "next") {
				t.Error("cursor flags")
			}
			return e
		}},
		{"GET", "/events/resource-health?project_id=p&task_id=t", "", `{"observed_at":"2026-10-05T12:00:00Z","samples":[],"truncated":false,"coverage":{"buffered":0,"capacity":100},"availability":"unavailable","warning_fraction":0.8,"clear_fraction":0.7}`, func(c *brain.Client) error {
			v, e := c.Events().ResourceHealth(ctx, url.Values{"project_id": {"p"}, "task_id": {"t"}})
			if e == nil && (v.Availability != "unavailable" || v.WarningFraction != 0.8) {
				t.Error("health availability")
			}
			return e
		}},
		{"GET", "/timeline?from=2026-10-01T00%3A00%3A00Z&project=p", "", `{"from":"2026-10-01T00:00:00Z","to":"2026-11-01T00:00:00Z","generated_at":"2026-10-05T12:00:00Z","items":null,"warnings":null,"truncated":false}`, func(c *brain.Client) error {
			v, e := c.Observability().Timeline(ctx, url.Values{"from": {"2026-10-01T00:00:00Z"}, "project": {"p"}})
			if e == nil && (v.Items != nil || v.Warnings != nil) {
				t.Error("timeline null")
			}
			return e
		}},
	}
	for _, tt := range cases {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			calls := 0
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tt.method || r.URL.RequestURI() != "/api/v1"+tt.path {
					t.Errorf("%s %s", r.Method, r.URL.RequestURI())
				}
				if tt.body != "" {
					var a, b any
					_ = json.Unmarshal([]byte(tt.body), &a)
					if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
						t.Error(e)
					}
					if !reflect.DeepEqual(a, b) {
						t.Errorf("body=%v expected=%v", b, a)
					}
				}
				_, _ = w.Write([]byte(tt.response))
			}, brain.Config{})
			if e := tt.call(c); e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Error("unexpected retry")
			}
		})
	}
}
