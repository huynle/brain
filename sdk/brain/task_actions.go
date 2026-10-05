package brain

import (
	"context"
	"net/url"
)

// Action methods do not retry or substitute another action. A 200 response can
// represent a no-op or partial result; inspect the returned outcome fields.
func (s TasksService) Resume(ctx context.Context, project, id string, request ResumeTaskOptions, options RequestOptions) (*ResumeTaskResult, error) {
	return result[ResumeTaskResult](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/resume", request, nil, options)
}
func (s TasksService) ResumeWithContext(ctx context.Context, project, id string, request ResumeWithContextOptions, options RequestOptions) (*ResumeWithContextResult, error) {
	return result[ResumeWithContextResult](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/resume-with-context", request, nil, options)
}
func (s TasksService) Assign(ctx context.Context, project, id string, request TaskAssignmentRequest, options RequestOptions) (*TaskAssignmentResponse, error) {
	return result[TaskAssignmentResponse](s.c, ctx, "PUT", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/assignment", request, nil, options)
}
func (s TasksService) ClearAssignment(ctx context.Context, project, id string, request ClearFeatureAssignmentRequest, options RequestOptions) (*TaskAssignmentResponse, error) {
	return result[TaskAssignmentResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/assignment/clear", request, nil, options)
}
func (s TasksService) Trigger(ctx context.Context, project, id string, options RequestOptions) (*TriggerResponse, error) {
	return result[TriggerResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/trigger", nil, nil, options)
}
func (s TasksService) Run(ctx context.Context, project, id string, request RunTaskRequest, options RequestOptions) (*RunTaskResponse, error) {
	return result[RunTaskResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/run", request, nil, options)
}
func (s TasksService) Dispatch(ctx context.Context, project, id string, request DispatchRequest, options RequestOptions) (*SDKDispatchResponse, error) {
	return result[SDKDispatchResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/dispatch", request, nil, options)
}

// Logs without offset returns the tail. offset=0 explicitly requests the head.
func (s TasksService) Logs(ctx context.Context, project, id string, query url.Values) (*LogQueryResponse, error) {
	return result[LogQueryResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/"+url.PathEscape(id)+"/logs", nil, query, RequestOptions{})
}
func (s ProjectsService) GetPlacement(ctx context.Context, project string) (*ProjectPlacement, error) {
	var out *ProjectPlacement
	err := s.c.request(ctx, "GET", "/projects/"+url.PathEscape(project)+"/placement", nil, nil, RequestOptions{}, &out)
	return out, err
}
func (s ProjectsService) SetPlacement(ctx context.Context, project string, request ProjectPlacement, options RequestOptions) (*ProjectPlacement, error) {
	return result[ProjectPlacement](s.c, ctx, "PUT", "/projects/"+url.PathEscape(project)+"/placement", request, nil, options)
}
func (s ProjectsService) Run(ctx context.Context, project string, request RunProjectRequest, options RequestOptions) (*RunProjectResponse, error) {
	return result[RunProjectResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/run", request, nil, options)
}
func (s FeaturesService) Resume(ctx context.Context, project, id string, request ResumeTaskOptions, options RequestOptions) (*ResumeFeatureResult, error) {
	return result[ResumeFeatureResult](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/resume", request, nil, options)
}
func (s FeaturesService) ResumeWithContext(ctx context.Context, project, id string, request ResumeWithContextOptions, options RequestOptions) (*ResumeWithContextFeatureResult, error) {
	return result[ResumeWithContextFeatureResult](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/resume-with-context", request, nil, options)
}
func (s FeaturesService) Assign(ctx context.Context, project, id string, request FeatureAssignmentRequest, options RequestOptions) (*FeatureAssignmentResponse, error) {
	return result[FeatureAssignmentResponse](s.c, ctx, "PUT", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/assignment", request, nil, options)
}
func (s FeaturesService) ClearAssignment(ctx context.Context, project, id string, request ClearFeatureAssignmentRequest, options RequestOptions) (*FeatureAssignmentResponse, error) {
	return result[FeatureAssignmentResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/assignment/clear", request, nil, options)
}
func (s FeaturesService) Checkout(ctx context.Context, project, id string, request FeatureCheckoutOptions, options RequestOptions) (*CheckoutFeatureResult, error) {
	return result[CheckoutFeatureResult](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/checkout", request, nil, options)
}
func (s FeaturesService) Run(ctx context.Context, project, id string, request RunFeatureRequest, options RequestOptions) (*RunFeatureResponse, error) {
	return result[RunFeatureResponse](s.c, ctx, "POST", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/run", request, nil, options)
}

// Cancel stops a dependent chain, never already-dispatched tasks.
func (s FeaturesService) Cancel(ctx context.Context, project, id string, options RequestOptions) (*CancelChainResponse, error) {
	return result[CancelChainResponse](s.c, ctx, "DELETE", "/tasks/"+url.PathEscape(project)+"/features/"+url.PathEscape(id)+"/run", nil, nil, options)
}
func (s FeaturesService) Chains(ctx context.Context, project string) (*DependentChainsResponse, error) {
	return result[DependentChainsResponse](s.c, ctx, "GET", "/tasks/"+url.PathEscape(project)+"/chains", nil, nil, RequestOptions{})
}
