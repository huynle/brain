package brain

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sdkTransportFunc func(*http.Request) (*http.Response, error)

func (f sdkTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicTransportPolicy(t *testing.T) {
	calls := 0
	next := sdkTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	transport, err := NewHTTPTransport("https://brain.invalid/prefix", next)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https://foreign.invalid/prefix/api/v1/health", "https://brain.invalid/other/api/v1/health", "https://brain.invalid/prefix/api/v1/entries/%252e%252e/secret"} {
		r, _ := http.NewRequest("GET", url, nil)
		resp, err := transport.RoundTrip(r)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Errorf("out-of-binding request admitted: %s", url)
		}
	}
	if calls != 0 {
		t.Fatalf("forbidden requests sent=%d", calls)
	}
}

func TestPublicTransportMutationAndRedirectPolicy(t *testing.T) {
	for _, method := range []string{"POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			next := sdkTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil || r.Body == nil || r.Body == http.NoBody {
					t.Error("mutation remains replayable")
				}
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://foreign.invalid"}}, Body: io.NopCloser(strings.NewReader("secret"))}, nil
			})
			tr, _ := NewHTTPTransport("https://brain.invalid", next)
			r, _ := http.NewRequest(method, "https://brain.invalid/api/v1/entries/id", nil)
			r.Header.Set("Idempotency-Key", "key")
			resp, err := tr.RoundTrip(r)
			if resp != nil {
				resp.Body.Close()
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != "redirect_refused" {
				t.Errorf("redirect result=%v", err)
			}
			if calls != 1 {
				t.Fatalf("round trips=%d", calls)
			}
			if r.Body != nil {
				t.Error("transport mutated caller request")
			}
		})
	}
}

func TestTypedSDKConsumesPublicTransport(t *testing.T) {
	c, _ := New(Config{BaseURL: "https://brain.invalid", Transport: sdkTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`))}, nil
	})})
	defer c.Close()
	if _, ok := c.http.Transport.(*HTTPTransport); !ok {
		t.Fatalf("typed SDK uses %T, not shared public transport", c.http.Transport)
	}
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicTransportCancellationAndSingleSend(t *testing.T) {
	calls := 0
	transportFailure := errors.New("lost response")
	next := sdkTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, transportFailure
	})
	tr, err := NewHTTPTransport("https://brain.invalid", next)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", "https://brain.invalid/api/v1/entries", strings.NewReader(`{}`))
	if _, err := tr.RoundTrip(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if calls != 0 {
		t.Fatal("canceled request sent")
	}
	r, _ = http.NewRequest("POST", "https://brain.invalid/api/v1/entries", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", "not-deduplicated")
	if _, err := tr.RoundTrip(r); !errors.Is(err, transportFailure) {
		t.Fatalf("transport error=%v", err)
	}
	if calls != 1 {
		t.Fatalf("ambiguous mutation sent %d times", calls)
	}
}
