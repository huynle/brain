package brain

import (
	"context"
	"net/url"
)

func (s TasksService) Ready(ctx context.Context, project string, filters *TasksReadyParams) (*TaskSelectionResponse, error) {
	q := url.Values{}
	if filters != nil {
		q = taskSelectionQuery(filters.FeatureId, filters.Executors, filters.RunnerId, filters.GeneratedByPrefix)
	}
	return result[TaskSelectionResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/ready", nil, q, RequestOptions{})
}

func (s TasksService) Next(ctx context.Context, project string, filters *TasksNextParams) (*ResolvedTask, error) {
	q := url.Values{}
	if filters != nil {
		q = taskSelectionQuery(filters.FeatureId, filters.Executors, filters.RunnerId, filters.GeneratedByPrefix)
	}
	var out *ResolvedTask
	err := s.c.request(ctx, "GET", "/tasks/"+url.PathEscape(project)+"/next", nil, q, RequestOptions{}, &out)
	return out, err
}

func taskSelectionQuery(features *TaskFeatureFilter, executors, runner, prefix *string) url.Values {
	q := url.Values{}
	if features != nil {
		for _, id := range *features {
			q.Add("feature_id", id)
		}
	}
	for k, v := range map[string]*string{"executors": executors, "runner_id": runner, "generated_by_prefix": prefix} {
		if v != nil {
			q.Set(k, *v)
		}
	}
	return q
}

func (s TasksService) Waiting(ctx context.Context, project string) (*TaskSelectionResponse, error) {
	return result[TaskSelectionResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/waiting", nil, nil, RequestOptions{})
}

func (s TasksService) Blocked(ctx context.Context, project string) (*TaskSelectionResponse, error) {
	return result[TaskSelectionResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/blocked", nil, nil, RequestOptions{})
}
