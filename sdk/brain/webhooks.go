package brain

import (
	"context"
	"net/url"
	"strconv"
)

type WebhooksService struct{ c *Client }

func (c *Client) Webhooks() WebhooksService { return WebhooksService{c} }
func (s WebhooksService) List(ctx context.Context, enabledOnly bool) (*ListWebhooksResponse, error) {
	return result[ListWebhooksResponse](s.c, ctx, "GET", "/webhooks", nil, url.Values{"enabled": {strconv.FormatBool(enabledOnly)}}, RequestOptions{})
}
func (s WebhooksService) Get(ctx context.Context, id string) (*WebhookResponse, error) {
	return result[WebhookResponse](s.c, ctx, "GET", "/webhooks/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
func (s WebhooksService) Create(ctx context.Context, r CreateWebhookRequest, o RequestOptions) (*WebhookResponse, error) {
	return result[WebhookResponse](s.c, ctx, "POST", "/webhooks", r, nil, o)
}
func (s WebhooksService) Update(ctx context.Context, id string, r UpdateWebhookRequest, o RequestOptions) (*WebhookResponse, error) {
	return result[WebhookResponse](s.c, ctx, "PATCH", "/webhooks/"+url.PathEscape(id), r, nil, o)
}
func (s WebhooksService) Delete(ctx context.Context, id string, o RequestOptions) (*SuccessResponse, error) {
	return result[SuccessResponse](s.c, ctx, "DELETE", "/webhooks/"+url.PathEscape(id), nil, nil, o)
}
func (s WebhooksService) Deliveries(ctx context.Context, id string, limit int) (*ListWebhookDeliveriesResponse, error) {
	return result[ListWebhookDeliveriesResponse](s.c, ctx, "GET", "/webhooks/"+url.PathEscape(id)+"/deliveries", nil, url.Values{"limit": {strconv.Itoa(limit)}}, RequestOptions{})
}
func (s WebhooksService) Test(ctx context.Context, id string, o RequestOptions) (*WebhookDeliveryResponse, error) {
	return result[WebhookDeliveryResponse](s.c, ctx, "POST", "/webhooks/"+url.PathEscape(id)+"/test", nil, nil, o)
}
