package brain_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const streamEvent = `{"id":"e1","type":"entry.created","source":"api","timestamp":"2026-10-05T12:00:00Z"}`

func TestEventStreamFramesAndStop(t *testing.T) {
	stop := errors.New("consumer stopped")
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/v1/events/stream?project_id=p+q" || r.Header.Get("Last-Event-ID") != "old" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("stream binding or filters lost")
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, ": heartbeat\r\n\r\nid: e1\r\nevent: entry.created\r\ndata: "+streamEvent+"\r\n\r\ndata: "+streamEvent+"\n\n")
	}, brain.Config{Token: "secret"})
	calls := 0
	e := c.Events().Stream(context.Background(), url.Values{"project_id": {"p q"}}, "old", func(v brain.Event) error {
		calls++
		if v.Id != "e1" || v.Source != "api" {
			t.Error("invalid event")
		}
		return stop
	}, brain.RequestOptions{})
	if !errors.Is(e, stop) || calls != 1 {
		t.Fatalf("stream=%v callbacks=%d", e, calls)
	}
}

func TestEventStreamBoundsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, ctype string
		status            int
		code              string
	}{
		{"oversized line", "data: " + strings.Repeat("x", 2048) + "\n\n", "text/event-stream", 200, "response_too_large"},
		{"oversized frame", strings.Repeat(": comment\n", 200) + "\n", "text/event-stream", 200, "response_too_large"},
		{"malformed", "data: nope\n\n", "text/event-stream", 200, "invalid_response"},
		{"null event", "data: null\n\n", "text/event-stream", 200, "invalid_response"},
		{"wrong content type", "{}", "application/json", 200, "invalid_response"},
		{"unauthorized", `{"message":"denied"}`, "application/json", 401, "unauthorized"},
		{"redirect", "", "text/event-stream", 302, "redirect_refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.Header().Set("X-Request-ID", "stream-request")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}, brain.Config{MaxResponseBytes: 1024})
			called := false
			e := c.Events().Stream(context.Background(), nil, "", func(brain.Event) error { called = true; return nil }, brain.RequestOptions{})
			var typed *brain.Error
			if !errors.As(e, &typed) || typed.Code != tc.code || typed.RequestID != "stream-request" || called {
				t.Fatalf("error=%+v called=%v", e, called)
			}
		})
	}
}

func TestEventStreamMultilineAndIncompleteEOF(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\n"+"data: \"id\":\"e1\",\"type\":\"entry.created\",\"source\":\"api\",\"timestamp\":\"2026-10-05T12:00:00Z\"}\n\n"+"data: "+streamEvent)
	}, brain.Config{})
	calls := 0
	e := c.Events().Stream(context.Background(), nil, "", func(brain.Event) error { calls++; return nil }, brain.RequestOptions{})
	if e != nil || calls != 1 {
		t.Fatalf("error=%v calls=%d", e, calls)
	}
}

func TestEventStreamCancellationAndRebind(t *testing.T) {
	for _, mode := range []string{"caller", "close", "rebind", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			ended := make(chan struct{})
			cfg := brain.Config{}
			if mode == "timeout" {
				cfg.Timeout = 50 * time.Millisecond
			}
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				defer close(ended)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
			}, cfg)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- c.Events().Stream(ctx, nil, "", func(brain.Event) error { t.Error("unexpected event"); return nil }, brain.RequestOptions{})
			}()
			select {
			case <-started:
			case e := <-done:
				t.Fatalf("ended before headers: %v", e)
			case <-time.After(time.Second):
				t.Fatal("no connection")
			}
			switch mode {
			case "caller":
				cancel()
			case "close":
				c.Close()
			case "rebind":
				next, e := c.Rebind(brain.Config{BaseURL: "http://example.test"})
				if e != nil {
					t.Fatal(e)
				}
				next.Close()
			}
			select {
			case e := <-done:
				if e == nil {
					t.Error("cancellation returned success")
				}
			case <-time.After(time.Second):
				t.Fatal("stream did not cancel")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("server request not closed")
			}
		})
	}
}
