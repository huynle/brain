package api

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The control request DTOs are unexported, so the public contract's request
// schemas are checked against their real JSON tags here: same property names,
// and the schema only requires what the handler cannot do without.
func TestControlRequestSchemasMatchHandlerDTOs(t *testing.T) {
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
	jsonFields := func(v any) []string {
		var out []string
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			out = append(out, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		}
		slices.Sort(out)
		return out
	}
	modelType := reflect.TypeOf(controlPromptRequest{}.Model).Elem()
	cases := []struct {
		schema   string
		fields   []string
		required []string
	}{
		{"ControlPromptRequest", jsonFields(controlPromptRequest{}), nil}, // text OR files: enforced by the handler
		{"ControlFilePart", jsonFields(controlFilePart{}), []string{"mime", "url"}},
		{"ControlPromptModel", jsonFields(reflect.New(modelType).Elem().Interface()), []string{"modelID", "providerID"}},
	}
	for _, tc := range cases {
		s, ok := doc.Components.Schemas[tc.schema]
		if !ok {
			t.Fatalf("missing schema %s", tc.schema)
		}
		var props []string
		for k := range s.Properties {
			props = append(props, k)
		}
		slices.Sort(props)
		if !slices.Equal(props, tc.fields) {
			t.Errorf("%s properties %v, handler DTO %v", tc.schema, props, tc.fields)
		}
		req := slices.Clone(s.Required)
		slices.Sort(req)
		if !slices.Equal(req, tc.required) {
			t.Errorf("%s required %v, want %v", tc.schema, req, tc.required)
		}
	}
}
