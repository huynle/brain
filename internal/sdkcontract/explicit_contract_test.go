package sdkcontract

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/sdk/brain"
	"gopkg.in/yaml.v3"
)

func TestMetadataContractExactAllowlist(t *testing.T) {
	b, e := os.ReadFile("../../api/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	var d struct {
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]any
				AdditionalProperties any `yaml:"additionalProperties"`
			}
		}
	}
	if e = yaml.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	s := d.Components.Schemas["MetadataUpdateRequest"]
	if s.AdditionalProperties != false {
		t.Fatal("metadata must be closed")
	}
	if len(s.Properties) != len(api.AllowedMetadataUpdateFields) {
		t.Fatalf("metadata keys=%d server=%d", len(s.Properties), len(api.AllowedMetadataUpdateFields))
	}
	for k := range api.AllowedMetadataUpdateFields {
		if _, ok := s.Properties[k]; !ok {
			t.Errorf("missing metadata key %s", k)
		}
	}
}

// The reflection bootstrap intentionally refuses embedded structs and nullable
// time marshalers. Explicit SDK shapes are instead checked using actual JSON.
func TestExplicitDTOJSONParity(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct{ source, target any }{
		{types.ResumeWithContextResult{ResumeTaskResult: types.ResumeTaskResult{TaskID: "t", Resumed: true, PriorStatus: "in_progress", PriorSessionsCount: 2, AbandonReason: "no_claim", Reason: "reason"}, ResumeMode: "live_injected", TargetSessionID: "s", InjectedLive: true}, &brain.ResumeWithContextResult{}},
		{types.ResumeWithContextFeatureResult{FeatureID: "f", TotalResumed: 1, TotalSkipped: 2, Truncated: true, TotalResults: 5, Results: []types.ResumeWithContextResult{{ResumeTaskResult: types.ResumeTaskResult{TaskID: "t", Resumed: true}, ResumeMode: "rehydrate"}}}, &brain.ResumeWithContextFeatureResult{}},
		{types.ResourceSample{SampledAt: now, Warning: false}, &brain.ResourceSample{}},
		{types.TimelineResponse{From: now, To: now, GeneratedAt: now, Items: []types.TimelineItem{{ID: "i", Type: "t", Source: "s", Timestamp: now, TemporalState: "projected", WindowStart: &now, WindowEnd: &now}}, Warnings: []types.TimelineWarning{{SourceID: "s", Message: "warning"}}}, &brain.TimelineResponse{}},
	}
	for _, tt := range cases {
		t.Run(reflect.TypeOf(tt.target).String(), func(t *testing.T) {
			data, e := json.Marshal(tt.source)
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(data, tt.target); e != nil {
				t.Fatal(e)
			}
			out, e := json.Marshal(tt.target)
			if e != nil {
				t.Fatal(e)
			}
			var a, b any
			_ = json.Unmarshal(data, &a)
			_ = json.Unmarshal(out, &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("wire loss\nsource=%s\nSDK=%s", data, out)
			}
		})
	}
}
