package api

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/supervision"
	"github.com/huynle/brain-api/internal/types"
	"gopkg.in/yaml.v3"
)

// Hand-written public schemas whose wire shape is a Go type the reflection
// parity test cannot model (unexported handler DTOs, json.RawMessage fields,
// nullable timestamps, generic type names) are checked here: same property
// names, and the response schemas require exactly the fields without
// omitempty. Request inputs list their required fields explicitly.
func TestStep3SchemasMatchHandlerTypes(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string       `yaml:"required"`
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	wire := func(v any) (fields, required []string) {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")
			fields = append(fields, tag[0])
			if !slices.Contains(tag[1:], "omitempty") {
				required = append(required, tag[0])
			}
		}
		slices.Sort(fields)
		slices.Sort(required)
		return fields, required
	}
	for _, tc := range []struct {
		schema   string
		dto      any
		required []string // nil: derive from omitempty (response shapes)
	}{
		{"SessionTailPage", supervision.SessionPage{}, nil},
		{"SessionTailRecord", supervision.SessionRecord{}, nil},
		{"SessionChildrenPage", supervision.ChildPage{}, nil},
		{"SessionChild", supervision.Child{}, nil},
		{"SupervisorCheckpoint", types.SupervisorCheckpoint{}, nil},
		{"SyncDiff", syncDiff{}, nil},
		{"SupervisorCheckpointInput", types.SupervisorCheckpoint{}, []string{"artifact", "id", "project"}},
		{"ExecutionBudgetInput", types.ExecutionBudget{}, []string{"id", "project"}},
		{"SupervisorOperationRequest", supervisorCommand{}, []string{"id", "operation"}},
	} {
		s, ok := doc.Components.Schemas[tc.schema]
		if !ok {
			t.Fatalf("missing schema %s", tc.schema)
		}
		fields, required := wire(tc.dto)
		if tc.required != nil {
			required = tc.required
		}
		var props []string
		for k := range s.Properties {
			props = append(props, k)
		}
		slices.Sort(props)
		if !slices.Equal(props, fields) {
			t.Errorf("%s properties %v, handler type %v", tc.schema, props, fields)
		}
		req := slices.Clone(s.Required)
		slices.Sort(req)
		if !slices.Equal(req, required) {
			t.Errorf("%s required %v, want %v", tc.schema, req, required)
		}
	}
}
