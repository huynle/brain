package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// rejectedField has the shape the service returns when it rejects one named
// field of an automation definition (see service automation validation).
type rejectedField struct {
	field   string
	message string
}

func (e *rejectedField) Error() string {
	return ErrInvalidInput.Error() + ": " + e.field + ": " + e.message
}

func (e *rejectedField) ValidationDetail() types.ValidationDetail {
	return types.ValidationDetail{Field: e.field, Message: e.message}
}

// assertRejection checks the status and, when wantError is set, the error
// name and the presence of wantField in the validation details.
func assertRejection(t *testing.T, resp *http.Response, wantStatus int, wantError, wantField string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d", resp.StatusCode, wantStatus)
	}
	if wantError == "" {
		return
	}
	body := decodeJSON[types.ErrorResponse](t, resp)
	if body.Error != wantError {
		t.Fatalf("error = %q, want %q", body.Error, wantError)
	}
	if wantField == "" {
		return
	}
	for _, d := range body.Details {
		if d.Field == wantField {
			return
		}
	}
	t.Fatalf("details = %v, want a detail for field %q", body.Details, wantField)
}

func TestCreateEntry_AutomationRejectionNamesField(t *testing.T) {
	named := &rejectedField{field: "trigger.schedule", message: "invalid cron expression"}
	tests := []struct {
		name       string
		saveErr    error
		wantStatus int
		wantError  string
		wantField  string
	}{
		{
			name:       "named field rejection is a validation error",
			saveErr:    named,
			wantStatus: http.StatusBadRequest,
			wantError:  "Validation Error",
			wantField:  "trigger.schedule",
		},
		{
			name:       "wrapped named field rejection is a validation error",
			saveErr:    fmt.Errorf("save automation: %w", named),
			wantStatus: http.StatusBadRequest,
			wantError:  "Validation Error",
			wantField:  "trigger.schedule",
		},
		{
			name:       "unnamed invalid input stays a bad request",
			saveErr:    fmt.Errorf("%w: title too long", ErrInvalidInput),
			wantStatus: http.StatusBadRequest,
			wantError:  "Bad Request",
		},
		{
			name:       "infrastructure failure stays a server error",
			saveErr:    errors.New("disk full"),
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{
				saveFunc: func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
					return nil, tt.saveErr
				},
			}
			srv := httptest.NewServer(newTestRouter(mock))
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/entries", "application/json", jsonBody(t, map[string]any{
				"type": "automation", "title": "Bad automation", "content": "c",
				"trigger": map[string]any{"type": "cron", "schedule": "every tuesday"},
			}))
			if err != nil {
				t.Fatalf("POST /entries failed: %v", err)
			}
			assertRejection(t, resp, tt.wantStatus, tt.wantError, tt.wantField)
		})
	}
}

func TestUpdateEntry_AutomationRejectionNamesField(t *testing.T) {
	named := &rejectedField{field: "extends", message: "parent automation \"nope1\" not found"}
	tests := []struct {
		name       string
		updateErr  error
		wantStatus int
		wantError  string
		wantField  string
	}{
		{
			name:       "named field rejection is a validation error",
			updateErr:  named,
			wantStatus: http.StatusBadRequest,
			wantError:  "Validation Error",
			wantField:  "extends",
		},
		{
			name:       "infrastructure failure stays a server error",
			updateErr:  errors.New("disk full"),
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{
				updateFunc: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
					return nil, tt.updateErr
				},
			}
			srv := httptest.NewServer(newTestRouter(mock))
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/entries/abc12def", jsonBody(t, map[string]any{
				"extends": "nope1",
			}))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("PATCH /entries failed: %v", err)
			}
			assertRejection(t, resp, tt.wantStatus, tt.wantError, tt.wantField)
		})
	}
}
