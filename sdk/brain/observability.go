package brain

import (
	"context"
	"net/url"
	"strconv"
)

type ProjectsService struct{ c *Client }
type ObservabilityService struct{ c *Client }

func (c *Client) Projects() ProjectsService           { return ProjectsService{c} }
func (c *Client) Observability() ObservabilityService { return ObservabilityService{c} }
func (s ProjectsService) List(ctx context.Context) (*ProjectListResponse, error) {
	return result[ProjectListResponse](s.c, ctx, "GET", "/tasks", nil, nil, RequestOptions{})
}

// Stats preserves the legacy single-mode host paths; it is not script-safe.
func (s ObservabilityService) Stats(ctx context.Context, project, projects string, global bool) (*StatsResponse, error) {
	return result[StatsResponse](s.c, ctx, "GET", "/stats", nil, url.Values{"project": {project}, "projects": {projects}, "global": {strconv.FormatBool(global)}}, RequestOptions{})
}
func (s ObservabilityService) Stale(ctx context.Context, project, entryType string, days, limit int) (*[]BrainEntry, error) {
	return result[[]BrainEntry](s.c, ctx, "GET", "/stale", nil, url.Values{"project": {project}, "type": {entryType}, "days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}}, RequestOptions{})
}
func (s GraphService) Orphans(ctx context.Context, project, entryType string, limit int) (*[]BrainEntry, error) {
	return result[[]BrainEntry](s.c, ctx, "GET", "/orphans", nil, url.Values{"project": {project}, "type": {entryType}, "limit": {strconv.Itoa(limit)}}, RequestOptions{})
}
