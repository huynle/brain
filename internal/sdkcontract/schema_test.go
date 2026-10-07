package sdkcontract

import (
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type schemaFixture struct {
	ID     string `json:"id"`
	Count  int    `json:"count,omitempty"`
	Hidden string `json:"-"`
}

func TestSchemasActualEntryTaskDTOs(t *testing.T) {
	roots := []reflect.Type{
		reflect.TypeOf(types.BrainEntry{}), reflect.TypeOf(types.CreateEntryRequest{}),
		reflect.TypeOf(types.CreateEntryResponse{}), reflect.TypeOf(types.UpdateEntryRequest{}),
		reflect.TypeOf(types.ListEntriesResponse{}), reflect.TypeOf(types.SearchRequest{}),
		reflect.TypeOf(types.SearchResponse{}), reflect.TypeOf(types.TaskListResponse{}),
		reflect.TypeOf(types.ResolvedTask{}),
	}
	schemas, err := Schemas(roots...)
	if err != nil {
		t.Fatalf("actual wire DTO rejected: %v", err)
	}
	for _, root := range roots {
		if schemas[root.Name()] == nil {
			t.Errorf("missing %s", root.Name())
		}
	}
}

type customJSON string

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`42`), nil }

type customWireFixture struct {
	Value customJSON `json:"value"`
}
type stringOptionFixture struct {
	Value int `json:"value,string"`
}
type embeddedFixture struct{ schemaFixture }

func TestSchemasRejectUnmodeledJSONSemantics(t *testing.T) {
	for _, value := range []any{customWireFixture{}, stringOptionFixture{}, embeddedFixture{}} {
		t.Run(reflect.TypeOf(value).Name(), func(t *testing.T) {
			if _, err := Schemas(reflect.TypeOf(value)); err == nil {
				t.Fatal("unmodeled JSON representation accepted")
			}
		})
	}
}

type recursiveFixture struct {
	Next    *recursiveFixture `json:"next,omitempty"`
	Names   []string          `json:"names"`
	Labels  map[string]bool   `json:"labels"`
	Value   any               `json:"value"`
	Created time.Time         `json:"created"`
	Bytes   []byte            `json:"bytes"`
	Ratio   float64           `json:"ratio"`
}

func TestSchemasRecursiveWireShapes(t *testing.T) {
	got, err := Schemas(reflect.TypeOf(recursiveFixture{}))
	if err != nil {
		t.Fatalf("recursive JSON DTO rejected: %v", err)
	}
	null := map[string]any{"type": "null"}
	nullable := func(s map[string]any) map[string]any { return map[string]any{"anyOf": []any{s, null}} }
	want := map[string]any{"recursiveFixture": map[string]any{
		"type":     "object",
		"required": []string{"names", "labels", "value", "created", "bytes", "ratio"},
		"properties": map[string]any{
			"next":    nullable(map[string]any{"$ref": "#/components/schemas/recursiveFixture"}),
			"names":   nullable(map[string]any{"type": "array", "items": map[string]any{"type": "string"}}),
			"labels":  nullable(map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "boolean"}}),
			"value":   map[string]any{},
			"created": map[string]any{"type": "string", "format": "date-time"},
			"bytes":   nullable(map[string]any{"type": "string", "contentEncoding": "base64"}),
			"ratio":   map[string]any{"type": "number"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire shape mismatch: got %#v, want %#v", got, want)
	}
}

func TestSchemasPreserveJSONFields(t *testing.T) {
	got, err := Schemas(reflect.TypeOf(schemaFixture{}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"schemaFixture": map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer"}},
		"required":   []string{"id"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON wire schemas mismatch: got %#v, want %#v", got, want)
	}
}
