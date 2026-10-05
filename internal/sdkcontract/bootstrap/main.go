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
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := yaml.NewEncoder(os.Stdout).Encode(schemas); err != nil {
		log.Fatal(err)
	}
}
