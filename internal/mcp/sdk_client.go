package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/huynle/brain-api/sdk/brain"
)

// sdkCall runs fn against a public Go SDK client bound to exactly what the
// request-scoped APIClient already carries: the same loopback API base and
// the same caller bearer token (none when the caller sent none). No identity,
// tenant selector or authority is added; the binding lives for this one call.
//
// Tool errors are what agents read, and the SDK deliberately keeps server
// messages out of (*brain.Error).Error(). So failures are re-rendered with the
// legacy client's exact text (see checkAPIError) from the raw HTTP outcome,
// observed by a transport hook scoped to this call's context.
func sdkCall[T any](ctx context.Context, c *APIClient, fn func(context.Context, *brain.Client) (*T, error)) (*T, error) {
	sc, err := c.sdkBinding()
	if err != nil {
		return nil, err
	}
	defer sc.Close()
	obs := &legacyOutcome{}
	out, err := fn(context.WithValue(ctx, legacyOutcomeKey{}, obs), sc)
	if err != nil {
		return nil, obs.legacyError(err)
	}
	return out, nil
}

// sdkDo is sdkCall for operations whose response body the tool ignores.
func sdkDo(ctx context.Context, c *APIClient, fn func(context.Context, *brain.Client) error) error {
	_, err := sdkCall(ctx, c, func(ctx context.Context, sc *brain.Client) (*struct{}, error) {
		return nil, fn(ctx, sc)
	})
	return err
}

func (c *APIClient) sdkBinding() (*brain.Client, error) {
	next := http.DefaultTransport
	timeout := c.httpClient.Timeout
	if c.httpClient.Transport != nil {
		next = c.httpClient.Transport
	}
	sc, err := brain.New(brain.Config{
		BaseURL:   c.baseURL,
		Token:     c.authToken,
		Timeout:   timeout,
		Transport: legacyOutcomeRecorder{next: next},
	})
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	return sc, nil
}

type legacyOutcomeKey struct{}

// legacyOutcome is the raw result of the last HTTP exchange made with a
// context carrying it: a transport failure, or an error status and body.
type legacyOutcome struct {
	mu           sync.Mutex
	transportErr error
	resp         *http.Response
	body         []byte
}

func (o *legacyOutcome) legacyError(sdkErr error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case o.transportErr != nil:
		return fmt.Errorf("request failed: %w", o.transportErr)
	case o.resp != nil:
		if err := checkAPIError(o.resp, o.body); err != nil {
			return err
		}
	}
	return sdkErr
}

// legacyOutcomeRecorder sits beneath the SDK's own transport policy. It never
// changes what is sent; it only records failures for legacyOutcome and
// re-buffers error bodies so the SDK still decodes them.
type legacyOutcomeRecorder struct{ next http.RoundTripper }

// maxLegacyErrorBody bounds the buffered error body (the SDK's own default
// response limit).
const maxLegacyErrorBody = 8 << 20

func (t legacyOutcomeRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	obs, _ := r.Context().Value(legacyOutcomeKey{}).(*legacyOutcome)
	resp, err := t.next.RoundTrip(r)
	if obs == nil {
		return resp, err
	}
	if err != nil {
		// Same shape net/http.Client gives the legacy client: Op "URL": err.
		obs.mu.Lock()
		obs.transportErr = &url.Error{Op: urlErrorOp(r.Method), URL: r.URL.String(), Err: err}
		obs.mu.Unlock()
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxLegacyErrorBody+1))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if readErr == nil {
			obs.mu.Lock()
			obs.resp = &http.Response{StatusCode: resp.StatusCode, Status: resp.Status}
			obs.body = body
			obs.mu.Unlock()
		}
	}
	return resp, nil
}

// urlErrorOp mirrors net/http's unexported helper ("GET" -> "Get").
func urlErrorOp(method string) string {
	if method == "" {
		return "Get"
	}
	return method[:1] + strings.ToLower(method[1:])
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}
