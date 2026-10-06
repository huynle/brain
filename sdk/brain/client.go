package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL, Token, Tenant, AuthGeneration string
	Timeout                                time.Duration
	MaxResponseBytes                       int64
	Transport                              http.RoundTripper
}
type Client struct {
	base, token, tenant, generation string
	http                            *http.Client
	limit                           int64
	ctx                             context.Context
	cancel                          context.CancelFunc
}
type RequestOptions struct{ RequestID, IdempotencyKey string }
type Error struct {
	Code, Message, RequestID string
	Status                   int
	Retryable                bool
	Details                  []FieldViolation
}

type FieldViolation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

var machineCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Error formats only the stable machine code and HTTP status. Server message,
// request ID and field details are deliberately omitted (explicit fields only);
// a Code that is not a machine code is never echoed.
func (e *Error) Error() string {
	code := ""
	if machineCode.MatchString(e.Code) {
		code = e.Code
	}
	switch {
	case code != "" && e.Status != 0:
		return fmt.Sprintf("brain: %s (HTTP %d)", code, e.Status)
	case code != "":
		return "brain: " + code
	case e.Status != 0:
		return fmt.Sprintf("brain: request failed (HTTP %d)", e.Status)
	}
	return "brain: request failed"
}

// New fixes the origin, credentials and optional organization selector for the
// lifetime of this client. A selector is not a grant. Transport is trusted code.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, &Error{Code: "invalid_configuration"}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes > 64<<20 {
		return nil, &Error{Code: "invalid_configuration"}
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 8 << 20
	}
	for _, s := range []string{cfg.Token, cfg.Tenant, cfg.AuthGeneration} {
		if strings.ContainsAny(s, "\r\n\x00") {
			return nil, &Error{Code: "invalid_configuration"}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	transport, err := NewHTTPTransport(cfg.BaseURL, cfg.Transport)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), token: cfg.Token, tenant: cfg.Tenant, generation: cfg.AuthGeneration, limit: cfg.MaxResponseBytes, ctx: ctx, cancel: cancel, http: &http.Client{Timeout: cfg.Timeout, Transport: transport}}, nil
}

// Close cancels outstanding requests and permanently retires this binding.
func (c *Client) Close() { c.cancel() }

// Rebind validates a new binding, then retires the old binding and its requests.
// It never migrates cursors, caches or idempotency keys to the new identity.
func (c *Client) Rebind(cfg Config) (*Client, error) {
	next, err := New(cfg)
	if err != nil {
		return nil, err
	}
	c.Close()
	return next, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, q url.Values, opts RequestOptions, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	var input io.Reader
	contentType := "application/json"
	if raw, ok := body.(rawBody); ok {
		input = bytes.NewReader(raw.data)
		contentType = raw.contentType
	} else if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &Error{Code: "invalid_request"}
		}
		input = bytes.NewReader(data)
	}
	u := c.base + "/api/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, input)
	if err != nil {
		return &Error{Code: "invalid_request"}
	}
	req.Header.Set("Accept", "application/json")
	if stream, ok := out.(*eventStream); ok {
		req.Header.Set("Accept", "text/event-stream")
		if stream.lastEventID != "" {
			req.Header.Set("Last-Event-ID", stream.lastEventID)
		}
	}
	if input != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.tenant != "" {
		req.Header.Set("X-Brain-Tenant", c.tenant)
	}
	if opts.RequestID != "" {
		req.Header.Set("X-Request-ID", opts.RequestID)
	}
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var policy *Error
		if errors.As(err, &policy) {
			return policy
		}
		return &Error{Code: "transport_error"}
	}
	defer resp.Body.Close()
	e := &Error{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-ID")}
	if stream, ok := out.(*eventStream); ok && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return stream.read(ctx, c.ctx, resp, c.limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.limit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if err != nil {
		e.Code = "response_read_failed"
		return e
	}
	if int64(len(data)) > c.limit {
		e.Code = "response_too_large"
		return e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var wire struct {
			Code      string           `json:"code"`
			Message   string           `json:"message"`
			Error     string           `json:"error"`
			RequestID string           `json:"request_id"`
			Details   []FieldViolation `json:"details"`
		}
		_ = json.Unmarshal(data, &wire)
		e.Message = wire.Message
		if e.Message == "" {
			e.Message = wire.Error
		}
		e.Code = wire.Code
		e.Details = wire.Details
		if e.RequestID == "" {
			e.RequestID = wire.RequestID
		}
		if !machineCode.MatchString(e.Code) {
			e.Code = ""
		}
		if e.Code == "" {
			e.Code = map[int]string{400: "invalid_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 409: "conflict", 429: "rate_limited", 501: "unsupported_operation", 503: "unavailable"}[resp.StatusCode]
			if e.Code == "" {
				e.Code = "http_error"
			}
		}
		e.Retryable = resp.StatusCode == 429 || resp.StatusCode == 503
		return e
	}
	if manifest, ok := out.(*capabilityBody); ok {
		if resp.StatusCode != http.StatusOK {
			e.Code = "unexpected_status"
			return e
		}
		*manifest = data
	} else if raw, ok := out.(*[]byte); ok {
		*raw = data
	} else if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			e.Code = "invalid_response"
			return e
		}
	}
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	return nil
}

func result[T any](c *Client, ctx context.Context, method, path string, body any, q url.Values, opts RequestOptions) (*T, error) {
	var out T
	if err := c.request(ctx, method, path, body, q, opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	return result[HealthResponse](c, ctx, "GET", "/health", nil, nil, RequestOptions{})
}
func (c *Client) Entries() EntriesService { return EntriesService{c} }
func (c *Client) Tasks() TasksService     { return TasksService{c} }
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	return result[SearchResponse](c, ctx, "POST", "/search", req, nil, RequestOptions{})
}

type EntriesService struct{ c *Client }
type TasksService struct{ c *Client }

func (s EntriesService) Get(ctx context.Context, id string) (*BrainEntry, error) {
	return result[BrainEntry](s.c, ctx, "GET", "/entries/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
func (s EntriesService) List(ctx context.Context, p *EntriesListParams) (*ListEntriesResponse, error) {
	return result[ListEntriesResponse](s.c, ctx, "GET", "/entries", nil, listQuery(p), RequestOptions{})
}

func listQuery(p *EntriesListParams) url.Values {
	q := url.Values{}
	if p != nil {
		for k, v := range map[string]*string{"project": p.Project, "type": p.Type, "status": p.Status, "tags": p.Tags, "feature_id": p.FeatureId, "sortBy": p.SortBy, "sortOrder": p.SortOrder} {
			if v != nil {
				q.Set(k, *v)
			}
		}
		if p.Limit != nil {
			q.Set("limit", strconv.Itoa(*p.Limit))
		}
		if p.Offset != nil {
			q.Set("offset", strconv.Itoa(*p.Offset))
		}
		if p.Global != nil {
			q.Set("global", strconv.FormatBool(*p.Global))
		}
	}
	return q
}
func (s EntriesService) Create(ctx context.Context, r CreateEntryRequest, o RequestOptions) (*CreateEntryResponse, error) {
	return result[CreateEntryResponse](s.c, ctx, "POST", "/entries", r, nil, o)
}
func (s EntriesService) Update(ctx context.Context, id string, r UpdateEntryRequest, o RequestOptions) (*BrainEntry, error) {
	return result[BrainEntry](s.c, ctx, "PATCH", "/entries/"+url.PathEscape(id), r, nil, o)
}

// Delete requires explicit method invocation and sends the REST confirmation.
// Force overrides the legacy live-claim guard; it defaults false at the call site.
func (s EntriesService) Delete(ctx context.Context, id string, force bool) error {
	q := url.Values{"confirm": {"true"}}
	if force {
		q.Set("force", "true")
	}
	return s.c.request(ctx, "DELETE", "/entries/"+url.PathEscape(id), nil, q, RequestOptions{}, nil)
}
func (s TasksService) List(ctx context.Context, project string) (*TaskListResponse, error) {
	return result[TaskListResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project), nil, nil, RequestOptions{})
}
func (s TasksService) Get(ctx context.Context, project, id string) (*ResolvedTask, error) {
	return result[ResolvedTask](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
