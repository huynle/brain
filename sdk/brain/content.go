package brain

import (
	"context"
	"net/url"
	"strconv"
)

func (s EntriesService) Move(ctx context.Context, id string, r MoveEntryRequest, o RequestOptions) (*MoveResult, error) {
	return result[MoveResult](s.c, ctx, "POST", "/entries/"+url.PathEscape(id)+"/move", r, nil, o)
}
func (s EntriesService) BulkUpdate(ctx context.Context, r BulkUpdateRequest, o RequestOptions) (*BulkUpdateResponse, error) {
	return result[BulkUpdateResponse](s.c, ctx, "POST", "/entries/bulk-update", r, nil, o)
}
func (s EntriesService) BulkDelete(ctx context.Context, r BulkDeleteRequest, o RequestOptions) (*BulkDeleteResponse, error) {
	return result[BulkDeleteResponse](s.c, ctx, "POST", "/entries/bulk-delete", r, nil, o)
}

type SectionsService struct{ c *Client }

func (c *Client) Sections() SectionsService { return SectionsService{c} }
func (s SectionsService) List(ctx context.Context, id string) (*SectionsResponse, error) {
	return result[SectionsResponse](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/sections", nil, nil, RequestOptions{})
}
func (s SectionsService) Get(ctx context.Context, id, title string, includeSubsections bool) (*SectionContentResponse, error) {
	return result[SectionContentResponse](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/sections/"+url.PathEscape(title), nil, url.Values{"includeSubsections": {strconv.FormatBool(includeSubsections)}}, RequestOptions{})
}

type GraphService struct{ c *Client }

func (c *Client) Graph() GraphService { return GraphService{c} }
func (s GraphService) Backlinks(ctx context.Context, id string) (*[]BrainEntry, error) {
	return result[[]BrainEntry](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/backlinks", nil, nil, RequestOptions{})
}
func (s GraphService) Outlinks(ctx context.Context, id string) (*[]BrainEntry, error) {
	return result[[]BrainEntry](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/outlinks", nil, nil, RequestOptions{})
}
func (s GraphService) Related(ctx context.Context, id string, limit int) (*[]BrainEntry, error) {
	return result[[]BrainEntry](s.c, ctx, "GET", "/entries/"+url.PathEscape(id)+"/related", nil, url.Values{"limit": {strconv.Itoa(limit)}}, RequestOptions{})
}
