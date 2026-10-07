package brain

import (
	"context"
	"net/url"
	"strconv"
)

type AttentionService struct{ c *Client }

func (c *Client) Attention() AttentionService { return AttentionService{c} }
func (s AttentionService) List(ctx context.Context, p *AttentionListParams) (*AttentionListResponse, error) {
	q := url.Values{}
	if p != nil {
		for k, v := range map[string]*string{"state": p.State, "project": p.Project, "kind": p.Kind, "severity": p.Severity, "source_type": p.SourceType} {
			if v != nil {
				q.Set(k, *v)
			}
		}
		if p.IncludeSnoozed != nil {
			q.Set("include_snoozed", strconv.FormatBool(*p.IncludeSnoozed))
		}
	}
	return result[AttentionListResponse](s.c, ctx, "GET", "/attention", nil, q, RequestOptions{})
}
func (s AttentionService) Get(ctx context.Context, id string) (*Attention, error) {
	return result[Attention](s.c, ctx, "GET", "/attention/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
func (s AttentionService) Counts(ctx context.Context) (*AttentionCounts, error) {
	return result[AttentionCounts](s.c, ctx, "GET", "/attention/counts", nil, nil, RequestOptions{})
}
func (s AttentionService) Create(ctx context.Context, r CreateAttentionRequest, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention", r, nil, o)
}
func (s AttentionService) Read(ctx context.Context, id string, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention/"+url.PathEscape(id)+"/read", nil, nil, o)
}
func (s AttentionService) Unread(ctx context.Context, id string, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention/"+url.PathEscape(id)+"/unread", nil, nil, o)
}
func (s AttentionService) Snooze(ctx context.Context, id string, r SnoozeAttentionRequest, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention/"+url.PathEscape(id)+"/snooze", r, nil, o)
}
func (s AttentionService) Resolve(ctx context.Context, id string, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention/"+url.PathEscape(id)+"/resolve", nil, nil, o)
}
func (s AttentionService) Dismiss(ctx context.Context, id string, o RequestOptions) (*Attention, error) {
	return result[Attention](s.c, ctx, "POST", "/attention/"+url.PathEscape(id)+"/dismiss", nil, nil, o)
}
