package brain

import (
	"context"
	"net/url"
)

// Status reads statuses immediately. Legacy waitFor/timeout fields do not wait.
func (s TasksService) Status(ctx context.Context, project string, request MultiTaskStatusRequest) (*MultiTaskStatusResponse, error) {
	return result[MultiTaskStatusResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/status", request, nil, RequestOptions{})
}

func (s TasksService) Metadata(ctx context.Context, project, id string) (*TaskMetadataResponse, error) {
	return result[TaskMetadataResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/metadata", nil, nil, RequestOptions{})
}

func (s TasksService) ClaimStatus(ctx context.Context, project, id string) (*ClaimStatusResponse, error) {
	return result[ClaimStatusResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/claim-status", nil, nil, RequestOptions{})
}

type FeaturesService struct{ c *Client }

func (c *Client) Features() FeaturesService { return FeaturesService{c} }

func (s FeaturesService) List(ctx context.Context, project string) (*FeatureListResponse, error) {
	return result[FeatureListResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/features", nil, nil, RequestOptions{})
}

func (s FeaturesService) Ready(ctx context.Context, project string) (*FeatureListResponse, error) {
	return result[FeatureListResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/features/ready", nil, nil, RequestOptions{})
}

func (s FeaturesService) Get(ctx context.Context, project, id string) (*FeatureResponse, error) {
	return result[FeatureResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
