package brain_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestBodylessMutationDoesNotReplayAfterLostResponse(t *testing.T) {
	for _, detach := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "detach"}[detach], func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{}`)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				attempts.Add(1)
				if r.Header.Get("Idempotency-Key") != "mutation-key" {
					t.Error("idempotency key missing")
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer srv.Close()
			c, err := brain.New(brain.Config{BaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if _, err := c.Health(context.Background()); err != nil {
				t.Fatal(err)
			}
			if detach {
				_, err = c.Attachments().Detach(context.Background(), "project", "entry", "attachment", "source", brain.RequestOptions{IdempotencyKey: "mutation-key"})
			} else {
				_, err = c.Attachments().Delete(context.Background(), "project", "attachment", brain.RequestOptions{IdempotencyKey: "mutation-key"})
			}
			if err == nil {
				t.Fatal("expected lost-response error")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("mutation attempts=%d, want 1", got)
			}
		})
	}
}

func TestStructuralDotIdentifiersNeverReachServer(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = io.WriteString(w, `{}`) }))
	defer srv.Close()
	c, err := brain.New(brain.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range []string{".", "..", "%2e%2e", "%252e%252e", "folder/../entry", `folder\..\entry`} {
		_, err := c.Tasks().Get(context.Background(), id, "health")
		if err == nil {
			t.Errorf("accepted structural identifier %q", id)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("server requests=%d, want 0", requests.Load())
	}
}
