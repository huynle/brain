package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func TestFeaturePickerModalAssignsThroughManualAssignmentAPI(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody types.FeatureAssignmentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.FeatureAssignmentResponse{
			ProjectID: "brain-api",
			FeatureID: "feature-auth",
			RunnerID:  "runner-1",
			Source:    "manual",
			Status:    "active",
		})
	}))
	defer srv.Close()

	modal := NewFeaturePickerModal(
		"runner-1",
		"brain-api",
		[]string{"feature-auth"},
		nil,
		runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}),
	)

	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Fatal("expected enter to be handled")
	}
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	result, ok := msg.(featureAssignmentResultMsg)
	if !ok {
		t.Fatalf("message type = %T, want featureAssignmentResultMsg", msg)
	}
	if result.err != nil {
		t.Fatalf("unexpected command error: %v", result.err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/tasks/brain-api/features/feature-auth/assignment" {
		t.Errorf("path = %q, want manual assignment endpoint", gotPath)
	}
	if gotBody.RunnerID != "runner-1" || gotBody.Intent != "assign" {
		t.Fatalf("body = %+v, want runner-1 assign", gotBody)
	}
}

func TestFeaturePickerModalReassignsAndClearsAssignment(t *testing.T) {
	paths := []string{}
	intents := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/tasks/brain-api/features/feature-auth/assignment":
			var body types.FeatureAssignmentRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode assign body: %v", err)
			}
			intents = append(intents, body.Intent)
			json.NewEncoder(w).Encode(types.FeatureAssignmentResponse{
				ProjectID:      "brain-api",
				FeatureID:      "feature-auth",
				RunnerID:       "runner-1",
				PreviousRunner: "runner-2",
				Source:         "manual",
				Status:         "active",
			})
		case "/api/v1/tasks/brain-api/features/feature-auth/assignment/clear":
			var body types.ClearFeatureAssignmentRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode clear body: %v", err)
			}
			intents = append(intents, body.Intent)
			json.NewEncoder(w).Encode(types.FeatureAssignmentResponse{
				ProjectID:      "brain-api",
				FeatureID:      "feature-auth",
				PreviousRunner: "runner-2",
				Source:         "manual",
				Status:         "cleared",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	modal := NewFeaturePickerModal(
		"runner-1",
		"brain-api",
		[]string{"feature-auth"},
		[]types.FeatureAssignmentResponse{{
			ProjectID: "brain-api",
			FeatureID: "feature-auth",
			RunnerID:  "runner-2",
			Source:    "manual",
			Status:    "active",
		}},
		runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}),
	)

	_, assignCmd := modal.HandleKey("enter")
	if assignCmd == nil {
		t.Fatal("expected reassign command")
	}
	if result := assignCmd().(featureAssignmentResultMsg); result.err != nil {
		t.Fatalf("unexpected reassign error: %v", result.err)
	}

	_, clearCmd := modal.HandleKey("c")
	if clearCmd == nil {
		t.Fatal("expected clear command")
	}
	if result := clearCmd().(featureAssignmentResultMsg); result.err != nil {
		t.Fatalf("unexpected clear error: %v", result.err)
	}

	if len(paths) != 2 {
		t.Fatalf("got %d requests, want 2", len(paths))
	}
	if intents[0] != "reassign" {
		t.Fatalf("first intent = %q, want reassign", intents[0])
	}
	if intents[1] != "clear" {
		t.Fatalf("second intent = %q, want clear", intents[1])
	}
}
