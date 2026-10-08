package sdkcontract

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The attention handler stores snoozed_until exactly as sent (empty and
// non-timestamp values included; the store never parses it), while the
// reminder handler validates remind_at as RFC 3339 with an offset and answers
// 400 with its own message. The contract describes those wire values, not an
// aspiration: only remind_at carries format date-time. The real handler's
// acceptance is exercised in exerciseNotificationSDK.
func TestSnoozeAttentionRequestDescribesTheWireContract(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string                  `yaml:"required"`
				Properties map[string]map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	until := doc.Components.Schemas["SnoozeAttentionRequest"].Properties["snoozed_until"]
	if until["type"] != "string" || until["format"] != nil || until["x-go-type"] != nil {
		t.Errorf("snoozed_until must be a plain string (stored verbatim, never parsed): %v", until)
	}
	remind := doc.Components.Schemas["SnoozeReminderRequest"].Properties["remind_at"]
	if remind["type"] != "string" || remind["format"] != "date-time" {
		t.Errorf("remind_at is validated as RFC 3339 by the server: %v", remind)
	}
}
