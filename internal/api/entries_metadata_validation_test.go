package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// fieldRejectedError is a service error that rejects one named request field,
// the shape automation lifecycle validation returns.
type fieldRejectedError struct{ field string }

func (e fieldRejectedError) Error() string {
	return ErrInvalidInput.Error() + ": " + e.field + ": rejected"
}

func (e fieldRejectedError) Is(target error) bool { return target == ErrInvalidInput }

func (e fieldRejectedError) ValidationDetail() types.ValidationDetail {
	return types.ValidationDetail{Field: e.field, Message: "rejected"}
}

// A field-level rejection from PATCH /entries/*/metadata must answer 400 and
// name the field in the validation details, not a bare message.
func TestHandleUpdateMetadata_FieldRejectionAnswers400WithField(t *testing.T) {
	mock := &mockBrainService{
		updateMetadataFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*types.BrainEntry, error) {
			return nil, fieldRejectedError{field: "starts_at"}
		},
	}
	srv := httptest.NewServer(newTestRouterWithEvents(mock, &mockEventService{}))
	defer srv.Close()

	body := jsonBody(t, map[string]any{"starts_at": "tomorrow"})
	req, _ := http.NewRequest("PATCH", srv.URL+"/entries/projects/myproj/automation/abc12def.md/metadata", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var got types.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Details) != 1 || got.Details[0].Field != "starts_at" {
		t.Fatalf("details = %+v, want one detail naming starts_at", got.Details)
	}
}
