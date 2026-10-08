// Command bootstrap prints candidate wire schemas for review when updating the
// public contract. It does not update OpenAPI or generate public SDKs.
package main

import (
	"log"
	"os"
	"reflect"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/sdkcontract"
	"github.com/huynle/brain-api/internal/types"
	"gopkg.in/yaml.v3"
)

func main() {
	schemas, err := sdkcontract.Schemas(
		reflect.TypeOf(types.BrainEntry{}), reflect.TypeOf(types.CreateEntryRequest{}),
		reflect.TypeOf(types.CreateEntryResponse{}), reflect.TypeOf(types.UpdateEntryRequest{}),
		reflect.TypeOf(types.ListEntriesResponse{}), reflect.TypeOf(types.SearchRequest{}),
		reflect.TypeOf(types.SearchResponse{}), reflect.TypeOf(types.TaskListResponse{}),
		reflect.TypeOf(types.ResolvedTask{}),
		reflect.TypeOf(api.TaskMetadataResponse{}), reflect.TypeOf(types.MultiTaskStatusRequest{}), reflect.TypeOf(types.MultiTaskStatusResponse{}), reflect.TypeOf(types.ClaimStatusResponse{}),
		reflect.TypeOf(types.FeatureListResponse{}), reflect.TypeOf(types.FeatureResponse{}),
		reflect.TypeOf(types.ProjectPlacement{}), reflect.TypeOf(types.DeleteProjectResponse{}),
		reflect.TypeOf(types.InjectRequest{}), reflect.TypeOf(types.InjectResponse{}),
		reflect.TypeOf(types.TaskAssignmentRequest{}), reflect.TypeOf(types.TaskAssignmentResponse{}),
		reflect.TypeOf(types.FeatureAssignmentRequest{}), reflect.TypeOf(types.FeatureAssignmentResponse{}), reflect.TypeOf(types.ClearFeatureAssignmentRequest{}),
		reflect.TypeOf(types.ResumeTaskOptions{}), reflect.TypeOf(types.ResumeTaskResult{}), reflect.TypeOf(types.ResumeFeatureResult{}),
		reflect.TypeOf(types.ResumeWithContextOptions{}), // Embedded result DTOs have explicit public schemas.
		reflect.TypeOf(types.RunTaskRequest{}), reflect.TypeOf(types.RunTaskResponse{}), reflect.TypeOf(types.RunFeatureRequest{}), reflect.TypeOf(types.RunFeatureResponse{}), reflect.TypeOf(types.RunProjectRequest{}), reflect.TypeOf(types.RunProjectResponse{}),
		reflect.TypeOf(types.FeatureCheckoutOptions{}), reflect.TypeOf(types.CheckoutFeatureResult{}), reflect.TypeOf(types.TriggerResponse{}),
		reflect.TypeOf(types.DependentChain{}), reflect.TypeOf(types.DispatchRequest{}), reflect.TypeOf(types.LogQueryResponse{}), reflect.TypeOf(types.DeliveryVerification{}),
		reflect.TypeOf(types.Event{}), reflect.TypeOf(types.EventCoverage{}), // Nullable timestamp DTOs require explicit schemas.
		reflect.TypeOf(types.MoveEntryRequest{}), reflect.TypeOf(types.MoveResult{}),
		reflect.TypeOf(types.BulkUpdateRequest{}), reflect.TypeOf(types.BulkUpdateResponse{}),
		reflect.TypeOf(types.BulkDeleteRequest{}), reflect.TypeOf(types.BulkDeleteResponse{}),
		reflect.TypeOf(types.SectionsResponse{}), reflect.TypeOf(types.SectionContentResponse{}),
		reflect.TypeOf(types.Attachment{}), reflect.TypeOf(types.CreateAttachmentResponse{}),
		reflect.TypeOf(types.ListAttachmentsResponse{}), reflect.TypeOf(types.AttachEntryAttachmentRequest{}),
		reflect.TypeOf(types.AttachEntryAttachmentResponse{}), reflect.TypeOf(types.AttachmentExtractionRequest{}),
		reflect.TypeOf(types.AttachmentExtractionResult{}),
		reflect.TypeOf(types.CreateGoalRequest{}), reflect.TypeOf(types.UpdateGoalRequest{}),
		reflect.TypeOf(types.GoalSummary{}), reflect.TypeOf(types.GoalProgressResponse{}), reflect.TypeOf(types.GoalReconcileAudit{}),
		reflect.TypeOf(types.CreateReminderRequest{}), reflect.TypeOf(types.UpdateReminderRequest{}), reflect.TypeOf(types.ReminderSummary{}), reflect.TypeOf(types.ReminderListResponse{}),
		reflect.TypeOf(types.CreateAttentionRequest{}), reflect.TypeOf(types.Attention{}), reflect.TypeOf(types.AttentionCounts{}),
		reflect.TypeOf(types.CreateWebhookRequest{}), reflect.TypeOf(types.UpdateWebhookRequest{}), reflect.TypeOf(types.WebhookResponse{}), reflect.TypeOf(types.WebhookDeliveryResponse{}),
		reflect.TypeOf(types.RunnerStatusResponse{}), reflect.TypeOf(types.RunnerListResponse{}), reflect.TypeOf(types.RunnerInfo{}),
		reflect.TypeOf(types.InstanceListResponse{}), reflect.TypeOf(types.SpawnInstanceSpec{}),
		reflect.TypeOf(types.DispatchLease{}), reflect.TypeOf(types.PlacementReasonListResponse{}), reflect.TypeOf(types.SchedulerStatus{}),
		reflect.TypeOf(types.CreateMonitorRequest{}), reflect.TypeOf(types.CreateMonitorResult{}), reflect.TypeOf(types.DeleteMonitorByScopeRequest{}), reflect.TypeOf(types.MonitorDeleteByScopeResponse{}),
		reflect.TypeOf(types.RunnerCandidatesResponse{}), reflect.TypeOf(types.TaskRunnerCandidatesRequest{}),
		reflect.TypeOf(types.ResolveClientContextRequest{}), reflect.TypeOf(types.ResolveClientContextResponse{}),
		reflect.TypeOf(types.SyncDevice{}), reflect.TypeOf(types.SupervisorOperation{}), reflect.TypeOf(types.ExecutionBudget{}), reflect.TypeOf(types.BudgetReservation{}),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := yaml.NewEncoder(os.Stdout).Encode(schemas); err != nil {
		log.Fatal(err)
	}
}
