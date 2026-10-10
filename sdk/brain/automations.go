package brain

import (
	"context"
	"net/url"
	"strconv"
)

type AutomationsService struct{ c *Client }

func (c *Client) Automations() AutomationsService { return AutomationsService{c} }
func (s AutomationsService) Run(ctx context.Context, r RunAutomationRequest, o RequestOptions) (*RunAutomationResponse, error) {
	return result[RunAutomationResponse](s.c, ctx, "POST", "/automations/run", r, nil, o)
}
func (s AutomationsService) Runs(ctx context.Context, p *AutomationsRunsParams) (*ListEntriesResponse, error) {
	q := url.Values{}
	if p != nil {
		for k, v := range map[string]*string{"project": p.Project, "status": p.Status, "automation_id": p.AutomationId} {
			if v != nil {
				q.Set(k, *v)
			}
		}
		if p.Limit != nil {
			q.Set("limit", strconv.Itoa(*p.Limit))
		}
	}
	return result[ListEntriesResponse](s.c, ctx, "GET", "/automation-runs", nil, q, RequestOptions{})
}
func (s AutomationsService) GetRun(ctx context.Context, id string) (*BrainEntry, error) {
	return result[BrainEntry](s.c, ctx, "GET", "/automation-runs/"+url.PathEscape(id), nil, nil, RequestOptions{})
}

// Effective returns the config project runs automation id under, with the
// binding overlaid, and whether the scheduler targets the project. project is
// required by the server, and id is escaped as one path segment.
func (s AutomationsService) Effective(ctx context.Context, id, project string) (*AutomationEffective, error) {
	q := url.Values{}
	q.Set("project", project)
	return result[AutomationEffective](s.c, ctx, "GET", "/automations/"+url.PathEscape(id)+"/effective", nil, q, RequestOptions{})
}
