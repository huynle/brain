// Command bootstrap prints candidate wire schemas for review when updating the
// public contract. It does not update OpenAPI or generate public SDKs.
package main

import (
	"log"
	"os"
	"reflect"

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
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := yaml.NewEncoder(os.Stdout).Encode(schemas); err != nil {
		log.Fatal(err)
	}
}
