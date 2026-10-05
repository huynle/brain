package sdkcontract

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"gopkg.in/yaml.v3"
)

func TestRepositoryContractAndWireParity(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	schemas, err := Schemas(reflect.TypeOf(types.BrainEntry{}), reflect.TypeOf(types.CreateEntryRequest{}), reflect.TypeOf(types.CreateEntryResponse{}), reflect.TypeOf(types.UpdateEntryRequest{}), reflect.TypeOf(types.ListEntriesResponse{}), reflect.TypeOf(types.SearchRequest{}), reflect.TypeOf(types.SearchResponse{}), reflect.TypeOf(types.TaskListResponse{}), reflect.TypeOf(types.ResolvedTask{}),
		reflect.TypeOf(types.MoveEntryRequest{}), reflect.TypeOf(types.MoveResult{}),
		reflect.TypeOf(types.BulkUpdateRequest{}), reflect.TypeOf(types.BulkUpdateResponse{}),
		reflect.TypeOf(types.BulkDeleteRequest{}), reflect.TypeOf(types.BulkDeleteResponse{}),
		reflect.TypeOf(types.SectionsResponse{}), reflect.TypeOf(types.SectionContentResponse{}),
		reflect.TypeOf(types.Attachment{}), reflect.TypeOf(types.CreateAttachmentResponse{}),
		reflect.TypeOf(types.ListAttachmentsResponse{}), reflect.TypeOf(types.AttachEntryAttachmentRequest{}),
		reflect.TypeOf(types.AttachEntryAttachmentResponse{}), reflect.TypeOf(types.AttachmentExtractionRequest{}),
		reflect.TypeOf(types.AttachmentExtractionResult{}),
		reflect.TypeOf(types.CreateGoalRequest{}), reflect.TypeOf(types.UpdateGoalRequest{}),
		reflect.TypeOf(types.ProjectListResponse{}), reflect.TypeOf(types.StatsResponse{}),
		reflect.TypeOf(api.TaskMetadataResponse{}), reflect.TypeOf(types.MultiTaskStatusRequest{}), reflect.TypeOf(types.MultiTaskStatusResponse{}), reflect.TypeOf(types.ClaimStatusResponse{}),
		reflect.TypeOf(types.FeatureListResponse{}), reflect.TypeOf(types.FeatureResponse{}),
		reflect.TypeOf(types.GoalSummary{}), reflect.TypeOf(types.GoalProgressResponse{}), reflect.TypeOf(types.GoalReconcileAudit{}),
		reflect.TypeOf(types.CreateReminderRequest{}), reflect.TypeOf(types.UpdateReminderRequest{}), reflect.TypeOf(types.ReminderSummary{}), reflect.TypeOf(types.ReminderListResponse{}),
		reflect.TypeOf(types.CreateAttentionRequest{}), reflect.TypeOf(types.Attention{}), reflect.TypeOf(types.AttentionCounts{}),
		reflect.TypeOf(types.CreateWebhookRequest{}), reflect.TypeOf(types.UpdateWebhookRequest{}), reflect.TypeOf(types.WebhookResponse{}), reflect.TypeOf(types.WebhookDeliveryResponse{}))
	if err != nil {
		t.Fatal(err)
	}
	for name, schema := range schemas {
		want, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(doc.Components.Schemas[name])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("wire schema drift for %s; review before updating the public contract", name)
		}
	}
}
