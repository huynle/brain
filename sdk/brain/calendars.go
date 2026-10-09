package brain

import "context"

// CalendarsService reads the status of the configured calendar sources.
type CalendarsService struct{ c *Client }

// Calendars returns the calendar status service.
func (c *Client) Calendars() CalendarsService { return CalendarsService{c} }

// List returns the status of every configured calendar source, sorted by name.
// A builtin source carries only its name and kind. No source reports a feed URL
// or file path.
func (s CalendarsService) List(ctx context.Context) (*CalendarsResponse, error) {
	return result[CalendarsResponse](s.c, ctx, "GET", "/calendars", nil, nil, RequestOptions{})
}
