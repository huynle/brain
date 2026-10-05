package brain

import (
	"context"
	"net/url"
	"strconv"
)

func (s EntriesService) UpdateMetadata(ctx context.Context, id string, request MetadataUpdateRequest, options RequestOptions) (*BrainEntry, error) {
	return result[BrainEntry](s.c, ctx, "PATCH", "/entries/"+url.PathEscape(id)+"/metadata", request, nil, options)
}
func (c *Client) Inject(ctx context.Context, request InjectRequest) (*InjectResponse, error) {
	return result[InjectResponse](c, ctx, "POST", "/inject", request, nil, RequestOptions{})
}

// Delete requires the caller to repeat the exact project in confirm. It deletes
// all entry types, not just tasks, and may return partial failures.
func (s ProjectsService) Delete(ctx context.Context, project, confirm string, force bool, options RequestOptions) (*DeleteProjectResponse, error) {
	return result[DeleteProjectResponse](s.c, ctx, "DELETE", "/tasks/"+url.PathEscape(project), nil, url.Values{"confirm": {confirm}, "force": {strconv.FormatBool(force)}}, options)
}
func (s TasksService) Delivery(ctx context.Context, project, id string) (*TaskDeliveryResponse, error) {
	return result[TaskDeliveryResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/delivery", nil, nil, RequestOptions{})
}

// VerifyDelivery never merges/deploys. Action verify may call the provider;
// inspect delivery.verification_error even on HTTP 200.
func (s TasksService) VerifyDelivery(ctx context.Context, project, id string, request DeliveryCommand, options RequestOptions) (*DeliveryUpdateResponse, error) {
	return result[DeliveryUpdateResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/delivery", request, nil, options)
}

type EventsService struct{ c *Client }

func (c *Client) Events() EventsService { return EventsService{c} }
func (s EventsService) Recent(ctx context.Context, query url.Values) (*RecentEventsResponse, error) {
	return result[RecentEventsResponse](s.c, ctx, "GET", "/events/recent", nil, query, RequestOptions{})
}
func (s EventsService) Wait(ctx context.Context, query url.Values) (*EventWaitResponse, error) {
	return result[EventWaitResponse](s.c, ctx, "GET", "/events/wait", nil, query, RequestOptions{})
}
func (s EventsService) ResourceHealth(ctx context.Context, query url.Values) (*ResourceHealthResponse, error) {
	return result[ResourceHealthResponse](s.c, ctx, "GET", "/events/resource-health", nil, query, RequestOptions{})
}
func (s ObservabilityService) Timeline(ctx context.Context, query url.Values) (*TimelineResponse, error) {
	return result[TimelineResponse](s.c, ctx, "GET", "/timeline", nil, query, RequestOptions{})
}
