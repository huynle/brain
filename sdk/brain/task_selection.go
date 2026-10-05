package brain

import (
	"context"
	"net/url"
)

func (s TasksService) Waiting(ctx context.Context, project string) (*TaskSelectionResponse, error) {
	return result[TaskSelectionResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/waiting", nil, nil, RequestOptions{})
}

func (s TasksService) Blocked(ctx context.Context, project string) (*TaskSelectionResponse, error) {
	return result[TaskSelectionResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/blocked", nil, nil, RequestOptions{})
}
