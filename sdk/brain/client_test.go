package brain_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestAmbiguousWriteIsNotReplayedByHTTPTransport(t *testing.T) {
	var writes atomic.Int32
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		writes.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}, brain.Config{})
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	} // Prime a reusable connection.
	_, err := c.Entries().Create(context.Background(), brain.CreateEntryRequest{Title: "uncertain"}, brain.RequestOptions{IdempotencyKey: "same-key"})
	if err == nil || writes.Load() != 1 {
		t.Fatalf("ambiguous POST replayed: writes=%d err=%v", writes.Load(), err)
	}
}

func client(t *testing.T, h http.HandlerFunc, cfg brain.Config) *brain.Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	cfg.BaseURL = s.URL
	c, err := brain.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestBoundAuthenticatedTypedEntry(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Brain-Tenant") != "org-a" {
			t.Error("binding headers missing")
		}
		if r.URL.EscapedPath() != "/api/v1/entries/projects%2Fx%2Fnote%2Fa.md" {
			t.Errorf("locator not escaped: %s", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"id":"a","title":"note","revision":"r1"}`))
	}, brain.Config{Token: "secret", Tenant: "org-a", AuthGeneration: "g1"})
	entry, err := c.Entries().Get(context.Background(), "projects/x/note/a.md")
	if err != nil || entry.Id != "a" || entry.Revision == nil || *entry.Revision != "r1" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}

func TestSingleModeOmitsTenantAndWriteOptions(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Brain-Tenant") != "" {
			t.Error("single mode sent tenant")
		}
		if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "key" || r.Header.Get("X-Request-ID") != "req" {
			t.Error("write options lost")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"created"}`))
	}, brain.Config{})
	entry, err := c.Entries().Create(context.Background(), brain.CreateEntryRequest{Type: "note", Title: "test", Content: "body"}, brain.RequestOptions{IdempotencyKey: "key", RequestID: "req"})
	if err != nil || entry.Id != "created" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}

func TestRefusesRedirectWithoutForwardingCredentials(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Store(true) }))
	defer other.Close()
	c := client(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }, brain.Config{Token: "secret"})
	_, err := c.Health(context.Background())
	var apiErr *brain.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "redirect_refused" || leaked.Load() {
		t.Fatalf("redirect boundary err=%v leaked=%v", err, leaked.Load())
	}
}

func TestBoundedResponseAndLegacyErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
		limit            int64
	}{
		{"large", strings.Repeat("x", 100), "response_too_large", 200, 32},
		{"legacy", `{"error":"Forbidden","message":"denied"}`, "forbidden", 403, 1000},
		{"malformed", `{`, "invalid_response", 200, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := client(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-ID", "server-req")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, brain.Config{MaxResponseBytes: tc.limit})
			_, err := c.Health(context.Background())
			var apiErr *brain.Error
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code || apiErr.RequestID != "server-req" {
				t.Fatalf("wrong error: %#v", err)
			}
		})
	}
}

func TestRebindCancelsAndDiscardsOldBinding(t *testing.T) {
	entered := make(chan struct{})
	c := client(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }, brain.Config{})
	done := make(chan error, 1)
	go func() { _, err := c.Health(context.Background()); done <- err }()
	<-entered
	next, err := c.Rebind(brain.Config{BaseURL: "https://example.invalid", Tenant: "b"})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old request not canceled")
	}
	_, err = c.Health(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("closed binding reused: %v", err)
	}
}

func TestCancellationAndNoWriteRetry(t *testing.T) {
	var calls atomic.Int32
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"message":"busy"}`))
	}, brain.Config{})
	_, err := c.Entries().Create(context.Background(), brain.CreateEntryRequest{}, brain.RequestOptions{IdempotencyKey: "k"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("write retries: calls=%d err=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Health(ctx)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("canceled request sent: %v", err)
	}
}

func TestTypedReadUpdateDeleteRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	project := "p"
	limit := 10
	revision := "r1"
	_, e1 := c.Entries().List(ctx, &brain.EntriesListParams{Project: &project, Limit: &limit})
	_, e2 := c.Entries().Update(ctx, "a", brain.UpdateEntryRequest{ExpectedRevision: &revision}, brain.RequestOptions{})
	e3 := c.Entries().Delete(ctx, "a", false)
	_, e4 := c.Tasks().List(ctx, "p")
	_, e5 := c.Tasks().Get(ctx, "p", "t")
	_, e6 := c.Search(ctx, brain.SearchRequest{})
	for _, err := range []error{e1, e2, e3, e4, e5, e6} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GET /api/v1/entries?limit=10&project=p", "PATCH /api/v1/entries/a", "DELETE /api/v1/entries/a?confirm=true", "GET /api/v1/tasks/p", "GET /api/v1/tasks/p/t", "POST /api/v1/search"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes=%q", got)
	}
}
