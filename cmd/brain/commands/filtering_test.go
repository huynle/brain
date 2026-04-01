package commands

import (
	"flag"
	"testing"
)

func TestBrainFilter_RegisterFlags(t *testing.T) {
	f := &BrainFilter{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.RegisterFlags(fs)

	// Verify all flags are registered
	expectedFlags := []string{
		"type", "status", "tags", "priority",
		"feature-id", "limit", "sort", "match", "m",
	}
	for _, name := range expectedFlags {
		if fs.Lookup(name) == nil {
			t.Errorf("expected flag %q to be registered", name)
		}
	}
}

func TestBrainFilter_RegisterFlags_Defaults(t *testing.T) {
	f := &BrainFilter{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.RegisterFlags(fs)

	// Parse with no args to get defaults
	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if f.Limit != 20 {
		t.Errorf("expected default limit 20, got %d", f.Limit)
	}
	if f.Sort != "" {
		t.Errorf("expected empty default sort, got %q", f.Sort)
	}
}

func TestBrainFilter_RegisterFlags_Parsing(t *testing.T) {
	f := &BrainFilter{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.RegisterFlags(fs)

	args := []string{
		"--type", "task",
		"--status", "pending",
		"--tags", "api,auth",
		"--priority", "high",
		"--feature-id", "feat-123",
		"--limit", "50",
		"--sort", "modified",
		"--match", "search query",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if f.Type != "task" {
		t.Errorf("Type = %q, want %q", f.Type, "task")
	}
	if f.Status != "pending" {
		t.Errorf("Status = %q, want %q", f.Status, "pending")
	}
	if f.Tags != "api,auth" {
		t.Errorf("Tags = %q, want %q", f.Tags, "api,auth")
	}
	if f.Priority != "high" {
		t.Errorf("Priority = %q, want %q", f.Priority, "high")
	}
	if f.FeatureID != "feat-123" {
		t.Errorf("FeatureID = %q, want %q", f.FeatureID, "feat-123")
	}
	if f.Limit != 50 {
		t.Errorf("Limit = %d, want %d", f.Limit, 50)
	}
	if f.Sort != "modified" {
		t.Errorf("Sort = %q, want %q", f.Sort, "modified")
	}
	if f.Match != "search query" {
		t.Errorf("Match = %q, want %q", f.Match, "search query")
	}
}

func TestBrainFilter_RegisterFlags_ShortMatch(t *testing.T) {
	f := &BrainFilter{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.RegisterFlags(fs)

	if err := fs.Parse([]string{"-m", "my search"}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if f.Match != "my search" {
		t.Errorf("Match = %q, want %q via -m short flag", f.Match, "my search")
	}
}

func TestBrainFilter_ToQueryParams_AllFields(t *testing.T) {
	f := &BrainFilter{
		Type:      "task",
		Status:    "pending",
		Tags:      "api,auth",
		Priority:  "high",
		FeatureID: "feat-123",
		Limit:     50,
		Sort:      "modified",
		Match:     "search query",
	}

	params := f.ToQueryParams()

	expected := map[string]string{
		"type":       "task",
		"status":     "pending",
		"tags":       "api,auth",
		"priority":   "high",
		"feature_id": "feat-123",
		"limit":      "50",
		"sortBy":     "modified",
		"query":      "search query",
	}

	for k, want := range expected {
		got, ok := params[k]
		if !ok {
			t.Errorf("missing key %q in query params", k)
			continue
		}
		if got != want {
			t.Errorf("params[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestBrainFilter_ToQueryParams_EmptyFields(t *testing.T) {
	f := &BrainFilter{}

	params := f.ToQueryParams()

	// Empty fields should not appear in params
	for _, key := range []string{"type", "status", "tags", "priority", "feature_id", "sortBy", "query"} {
		if _, ok := params[key]; ok {
			t.Errorf("unexpected key %q in params for empty filter", key)
		}
	}

	// Limit 0 should not appear (default is used by caller)
	if _, ok := params["limit"]; ok {
		t.Error("limit should not be in params when 0")
	}
}

func TestBrainFilter_ToQueryParams_PartialFields(t *testing.T) {
	f := &BrainFilter{
		Type:  "plan",
		Limit: 10,
	}

	params := f.ToQueryParams()

	if params["type"] != "plan" {
		t.Errorf("params[type] = %q, want %q", params["type"], "plan")
	}
	if params["limit"] != "10" {
		t.Errorf("params[limit] = %q, want %q", params["limit"], "10")
	}

	// Should not have empty fields
	if _, ok := params["status"]; ok {
		t.Error("unexpected status in partial params")
	}
}

func TestBrainFilter_Validate_ValidValues(t *testing.T) {
	tests := []struct {
		name   string
		filter BrainFilter
	}{
		{"empty", BrainFilter{}},
		{"valid type", BrainFilter{Type: "task"}},
		{"valid status", BrainFilter{Status: "pending"}},
		{"valid priority", BrainFilter{Priority: "high"}},
		{"valid sort", BrainFilter{Sort: "modified"}},
		{"all valid", BrainFilter{
			Type:     "plan",
			Status:   "active",
			Priority: "medium",
			Sort:     "created",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.filter.Validate(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBrainFilter_Validate_InvalidType(t *testing.T) {
	f := BrainFilter{Type: "bogus"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBrainFilter_Validate_InvalidStatus(t *testing.T) {
	f := BrainFilter{Status: "bogus"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestBrainFilter_Validate_InvalidPriority(t *testing.T) {
	f := BrainFilter{Priority: "bogus"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
}

func TestBrainFilter_Validate_InvalidSort(t *testing.T) {
	f := BrainFilter{Sort: "bogus"}
	err := f.Validate()
	if err == nil {
		t.Fatal("expected error for invalid sort")
	}
}

func TestBrainFilter_Validate_ValidSortValues(t *testing.T) {
	for _, sort := range []string{"created", "modified", "priority"} {
		f := BrainFilter{Sort: sort}
		if err := f.Validate(); err != nil {
			t.Errorf("unexpected error for sort=%q: %v", sort, err)
		}
	}
}
