package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	var out *T
	_, err := sdkObserved(ctx, c, func(ctx context.Context, sc *brain.Client) error {
		var err error
		out, err = fn(ctx, sc)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sdkObserved runs fn against a per-call binding and returns the outcome the
// transport observed, with fn's error rendered as legacy text.
func sdkObserved(ctx context.Context, c *APIClient, fn func(context.Context, *brain.Client) error) (*legacyOutcome, error) {
	sc, err := c.sdkBinding()
	if err != nil {
		return nil, err
	}
	defer sc.Close()
	obs := &legacyOutcome{}
	if err := fn(context.WithValue(ctx, legacyOutcomeKey{}, obs), sc); err != nil {
		return nil, obs.legacyError(err)
	}
	return obs, nil
}

// sdkRaw runs an SDK call for a tool that passes the API's JSON through
// verbatim, as its hand-built request did. The SDK still binds the caller's
// credential, refuses unsafe paths, maps errors and checks that the body is
// the operation's declared response; the tool then renders the body exactly
// as the legacy client decoded it (json.RawMessage, nil for an empty body).
func sdkRaw(ctx context.Context, c *APIClient, fn func(context.Context, *brain.Client) error) (json.RawMessage, error) {
	obs, err := sdkObserved(ctx, c, fn)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := decodeLegacyBody(obs.successBody(), &raw); err != nil {
		return nil, err
	}
	return raw, nil
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
// context carrying it: a transport failure, an error status and body, or the
// body of a success as far as the SDK read it.
type legacyOutcome struct {
	mu           sync.Mutex
	transportErr error
	resp         *http.Response
	body         []byte
	okBody       []byte
	ok           bool
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
	case o.ok:
		// A success body that is not JSON: the legacy client reported the
		// JSON syntax error where the SDK reports invalid_response.
		var be *brain.Error
		if errors.As(sdkErr, &be) && be.Code == "invalid_response" && len(o.okBody) > 0 {
			if err := json.Unmarshal(o.okBody, new(any)); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}
	}
	return sdkErr
}

func (o *legacyOutcome) successBody() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.okBody
}

// capturedBody copies what the SDK reads of a success body, bounded like the
// error body; reads are otherwise untouched.
type capturedBody struct {
	io.ReadCloser
	obs *legacyOutcome
}

func (b capturedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.obs.mu.Lock()
	if room := maxLegacyErrorBody + 1 - len(b.obs.okBody); room > 0 {
		b.obs.okBody = append(b.obs.okBody, p[:min(n, room)]...)
	}
	b.obs.mu.Unlock()
	return n, err
}

// legacyOutcomeRecorder sits beneath the SDK's own transport policy. It never
// changes what is sent; it records failures for legacyOutcome, re-buffers
// error bodies so the SDK still decodes them, and copies success bodies as
// the SDK reads them.
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
	if resp.StatusCode < 300 {
		obs.mu.Lock()
		obs.ok, obs.okBody = true, nil
		obs.mu.Unlock()
		resp.Body = capturedBody{ReadCloser: resp.Body, obs: obs}
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

// fromSDK copies an SDK DTO into the internal/types shape the existing
// formatters read. Both are generated from / pinned to the same wire schema
// (internal/sdkcontract parity test), so a JSON round trip is lossless and
// keeps agent-facing text unchanged.
func fromSDK[T any](v any) (T, error) {
	var out T
	data, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	if err != nil {
		return out, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

// decodeLegacyBody reproduces the legacy client's handling of a proxied
// response body: decode only when non-empty, with the same error text.
func decodeLegacyBody(raw []byte, out any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// sdkRead runs an SDK call and converts its DTO with fromSDK.
func sdkRead[T any](ctx context.Context, c *APIClient, fn func(context.Context, *brain.Client) (any, error)) (T, error) {
	var zero T
	out, err := sdkCall(ctx, c, func(ctx context.Context, sc *brain.Client) (*any, error) {
		v, err := fn(ctx, sc)
		if err != nil {
			return nil, err
		}
		return &v, nil
	})
	if err != nil {
		return zero, err
	}
	return fromSDK[T](*out)
}
