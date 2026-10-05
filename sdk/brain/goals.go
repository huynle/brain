package brain

import (
	"context"
	"net/url"
	"strconv"
)

type GoalsService struct{ c *Client }

func (c *Client) Goals() GoalsService { return GoalsService{c} }
func (s GoalsService) List(ctx context.Context, p *GoalsListParams) (*ListGoalsResponse, error) {
	q := url.Values{}
	if p != nil {
		for k, v := range map[string]*string{"project": p.Project, "feature_id": p.FeatureId, "status": p.Status} {
			if v != nil {
				q.Set(k, *v)
			}
		}
	}
	return result[ListGoalsResponse](s.c, ctx, "GET", "/goals", nil, q, RequestOptions{})
}
func (s GoalsService) Create(ctx context.Context, r CreateGoalRequest, o RequestOptions) (*GoalSummary, error) {
	return result[GoalSummary](s.c, ctx, "POST", "/goals", r, nil, o)
}
func (s GoalsService) Update(ctx context.Context, id string, r UpdateGoalRequest, o RequestOptions) (*GoalSummary, error) {
	return result[GoalSummary](s.c, ctx, "PATCH", "/goals/"+url.PathEscape(id), r, nil, o)
}
func (s GoalsService) Delete(ctx context.Context, id string, o RequestOptions) (*DeleteGoalResponse, error) {
	return result[DeleteGoalResponse](s.c, ctx, "DELETE", "/goals/"+url.PathEscape(id), nil, nil, o)
}
func (s GoalsService) Progress(ctx context.Context, id string) (*GoalProgressResponse, error) {
	return result[GoalProgressResponse](s.c, ctx, "GET", "/goals/"+url.PathEscape(id)+"/progress", nil, nil, RequestOptions{})
}
func (s GoalsService) Audit(ctx context.Context, id string, limit int) (*GoalAuditResponse, error) {
	return result[GoalAuditResponse](s.c, ctx, "GET", "/goals/"+url.PathEscape(id)+"/audit", nil, url.Values{"limit": {strconv.Itoa(limit)}}, RequestOptions{})
}
func (s GoalsService) Run(ctx context.Context, id string, o RequestOptions) (*GoalReconcileAudit, error) {
	return result[GoalReconcileAudit](s.c, ctx, "POST", "/goals/"+url.PathEscape(id)+"/run", nil, nil, o)
}
