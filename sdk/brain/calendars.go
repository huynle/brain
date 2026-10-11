package brain

import (
	"context"
	"net/url"
)

// CalendarsService reads and manages the calendar sources. Writes are
// admin-only and are not available to scripts: the address of a runtime source
// is a secret that the server stores privately and never returns.
type CalendarsService struct{ c *Client }

// Calendars returns the calendar service.
func (c *Client) Calendars() CalendarsService { return CalendarsService{c} }

// List returns the status of every configured calendar source, sorted by name.
// A builtin source carries only its name and kind. No source reports a feed URL
// or file path.
func (s CalendarsService) List(ctx context.Context) (*CalendarsResponse, error) {
	return result[CalendarsResponse](s.c, ctx, "GET", "/calendars", nil, nil, RequestOptions{})
}

// Create adds a runtime ICS calendar source (admin only). The server validates
// the address with one fetch and parse before it saves anything, so the source
// has events at once. A zero poll means the server default of five minutes. The
// returned status never carries the address.
func (s CalendarsService) Create(ctx context.Context, req CreateCalendarRequest) (*CalendarStatus, error) {
	return result[CalendarStatus](s.c, ctx, "POST", "/calendars", req, nil, RequestOptions{})
}

// Update replaces a runtime source's address, its poll interval, or both (admin
// only). At least one field must be set. A config source is refused.
func (s CalendarsService) Update(ctx context.Context, name string, req UpdateCalendarRequest) (*CalendarStatus, error) {
	return result[CalendarStatus](s.c, ctx, "PUT", "/calendars/"+url.PathEscape(name), req, nil, RequestOptions{})
}

// Delete removes a runtime source (admin only) and deletes its stored address.
// A source that automations still reference is refused with a conflict unless
// force is set. The response lists the references the removal affected.
func (s CalendarsService) Delete(ctx context.Context, name string, force bool) (*RemoveCalendarResponse, error) {
	var q url.Values
	if force {
		q = url.Values{"force": {"true"}}
	}
	return result[RemoveCalendarResponse](s.c, ctx, "DELETE", "/calendars/"+url.PathEscape(name), nil, q, RequestOptions{})
}
