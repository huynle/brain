package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsValidEntryType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"summary", true},
		{"report", true},
		{"walkthrough", true},
		{"plan", true},
		{"pattern", true},
		{"learning", true},
		{"idea", true},
		{"scratch", true},
		{"decision", true},
		{"exploration", true},
		{"execution", true},
		{"task", true},
		{"dream", true},
		{"automation", true},
		{"automation_run", true},
		{"invalid", false},
		{"", false},
		{"SUMMARY", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidEntryType(tt.input)
			if got != tt.want {
				t.Errorf("IsValidEntryType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidEntryStatus(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"draft", true},
		{"pending", true},
		{"active", true},
		{"in_progress", true},
		{"blocked", true},
		{"cancelled", true},
		{"completed", true},
		{"validated", true},
		{"superseded", true},
		{"archived", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidEntryStatus(tt.input)
			if got != tt.want {
				t.Errorf("IsValidEntryStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidPriority(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"high", true},
		{"medium", true},
		{"low", true},
		{"critical", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidPriority(tt.input)
			if got != tt.want {
				t.Errorf("IsValidPriority(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidTaskClassification(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ready", true},
		{"waiting", true},
		{"blocked", true},
		{"not_pending", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidTaskClassification(tt.input)
			if got != tt.want {
				t.Errorf("IsValidTaskClassification(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEntryTypeConstants(t *testing.T) {
	// Verify the count matches TypeScript source (13 types + automation + automation_run)
	if len(EntryTypes) != 15 {
		t.Errorf("expected 15 entry types, got %d", len(EntryTypes))
	}
}

func TestEntryStatusConstants(t *testing.T) {
	// Verify the count matches TypeScript source (10 statuses)
	if len(EntryStatuses) != 10 {
		t.Errorf("expected 10 entry statuses, got %d", len(EntryStatuses))
	}
}

func TestAttachmentEntryDTOsUseTypedReferences(t *testing.T) {
	entry := BrainEntry{
		ID:      "entry-1",
		Path:    "projects/demo/task/entry-1.md",
		Title:   "Entry with attachment",
		Type:    "task",
		Status:  "active",
		Content: "see attachment",
		Attachments: []AttachmentReference{
			{
				ID:          "att-1",
				Filename:    "diagram.png",
				ContentType: "image/png",
				Size:        12345,
				SHA256:      "abc123",
				Role:        "diagram",
				Derived: []AttachmentDerived{
					{ID: "derived-1", Kind: "thumbnail", ContentType: "image/png", Size: 512},
				},
			},
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(BrainEntry) error = %v", err)
	}

	jsonText := string(data)
	if !strings.Contains(jsonText, `"attachments"`) {
		t.Fatalf("BrainEntry JSON missing attachments: %s", jsonText)
	}
	if strings.Contains(jsonText, "base64") || strings.Contains(jsonText, `"data"`) {
		t.Fatalf("attachment reference JSON must not expose canonical binary/base64 data: %s", jsonText)
	}

	var decoded BrainEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(BrainEntry) error = %v", err)
	}
	if len(decoded.Attachments) != 1 {
		t.Fatalf("decoded attachments len = %d, want 1", len(decoded.Attachments))
	}
	if decoded.Attachments[0].ID != "att-1" {
		t.Errorf("decoded attachment ID = %q, want %q", decoded.Attachments[0].ID, "att-1")
	}
	if len(decoded.Attachments[0].Derived) != 1 || decoded.Attachments[0].Derived[0].Kind != "thumbnail" {
		t.Errorf("decoded derived attachment = %#v, want thumbnail", decoded.Attachments[0].Derived)
	}
}

func TestCreateUpdateEntryRequestsCarryAttachmentReferences(t *testing.T) {
	createJSON := []byte(`{
		"type":"report",
		"title":"With attachment",
		"content":"body",
		"attachments":[{"id":"att-1","filename":"notes.pdf","content_type":"application/pdf"}]
	}`)

	var createReq CreateEntryRequest
	if err := json.Unmarshal(createJSON, &createReq); err != nil {
		t.Fatalf("json.Unmarshal(CreateEntryRequest) error = %v", err)
	}
	if len(createReq.Attachments) != 1 || createReq.Attachments[0].ID != "att-1" {
		t.Fatalf("CreateEntryRequest attachments = %#v, want att-1 reference", createReq.Attachments)
	}

	updateJSON := []byte(`{"attachments":[{"id":"att-2","role":"source"}]}`)
	var updateReq UpdateEntryRequest
	if err := json.Unmarshal(updateJSON, &updateReq); err != nil {
		t.Fatalf("json.Unmarshal(UpdateEntryRequest) error = %v", err)
	}
	if updateReq.Attachments == nil {
		t.Fatal("UpdateEntryRequest attachments pointer is nil, want explicit update")
	}
	if len(*updateReq.Attachments) != 1 || (*updateReq.Attachments)[0].ID != "att-2" {
		t.Fatalf("UpdateEntryRequest attachments = %#v, want att-2 reference", updateReq.Attachments)
	}
}
