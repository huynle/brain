package brain

import (
	"context"
	"net/url"
)

type RemindersService struct{ c *Client }

func (c *Client) Reminders() RemindersService { return RemindersService{c} }
func (s RemindersService) List(ctx context.Context, p *RemindersListParams) (*ReminderListResponse, error) {
	q := url.Values{}
	if p != nil {
		if p.Project != nil {
			q.Set("project", *p.Project)
		}
		if p.State != nil {
			q.Set("state", *p.State)
		}
	}
	return result[ReminderListResponse](s.c, ctx, "GET", "/reminders", nil, q, RequestOptions{})
}
func (s RemindersService) Get(ctx context.Context, id string) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "GET", "/reminders/"+url.PathEscape(id), nil, nil, RequestOptions{})
}
func (s RemindersService) Create(ctx context.Context, r CreateReminderRequest, o RequestOptions) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "POST", "/reminders", r, nil, o)
}
func (s RemindersService) Update(ctx context.Context, id string, r UpdateReminderRequest, o RequestOptions) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "PATCH", "/reminders/"+url.PathEscape(id), r, nil, o)
}
func (s RemindersService) Delete(ctx context.Context, id string, o RequestOptions) (*DeletionResponse, error) {
	return result[DeletionResponse](s.c, ctx, "DELETE", "/reminders/"+url.PathEscape(id), nil, nil, o)
}
func (s RemindersService) Ack(ctx context.Context, id string, o RequestOptions) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "POST", "/reminders/"+url.PathEscape(id)+"/ack", nil, nil, o)
}
func (s RemindersService) Snooze(ctx context.Context, id string, r SnoozeReminderRequest, o RequestOptions) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "POST", "/reminders/"+url.PathEscape(id)+"/snooze", r, nil, o)
}
func (s RemindersService) Fire(ctx context.Context, id string, o RequestOptions) (*ReminderSummary, error) {
	return result[ReminderSummary](s.c, ctx, "POST", "/reminders/"+url.PathEscape(id)+"/fire", nil, nil, o)
}
