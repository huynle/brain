package sdkcontract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"gopkg.in/yaml.v3"
)

// The service seam induces the legacy conflict, but the real handler produces
// the wire response. This proves schema compatibility, not service authorization.
type conflictDispatch struct{ api.TaskService }

func (conflictDispatch) DispatchTask(context.Context, string, string, string) (*types.DispatchResponse, error) {
	return nil, api.ErrConflict
}

func TestLegacyErrorContractAcceptsActualDispatchConflict(t *testing.T) {
	h := api.NewHandler(nil, api.WithTaskService(conflictDispatch{}))
	router := chi.NewRouter()
	router.Post("/tasks/{projectId}/{taskId}/dispatch", h.HandleDispatchTask)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tasks/p/t/dispatch", strings.NewReader(`{"targetRunnerId":"other"}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected legacy409, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct{ Required []string }
		}
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	required := doc.Components.Schemas["LegacyErrorResponse"].Required
	if len(required) == 0 {
		t.Fatal("error schema lost required error member")
	}
	for _, key := range required {
		if _, ok := body[key]; !ok {
			t.Errorf("actual dispatch409 lacks schema-required %q: %s", key, w.Body.String())
		}
	}
}

// Response declarations are descriptive; they grant no retry or script rights.
func TestEveryOperationDeclaresLegacyErrorBehavior(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range doc.Paths {
		for method, node := range path {
			if method == "parameters" {
				continue
			}
			var op struct {
				ID        string `yaml:"operationId"`
				Responses map[string]struct {
					Content map[string]struct {
						Schema struct {
							Ref string `yaml:"$ref"`
						}
					}
				}
			}
			if err := node.Decode(&op); err != nil {
				t.Fatal(err)
			}
			count++
			if got := op.Responses["default"].Content["application/json"].Schema.Ref; got != "#/components/schemas/LegacyErrorResponse" {
				t.Errorf("%s missing declared legacy error response: %q", op.ID, got)
			}
		}
	}
	if count != 146 {
		t.Fatalf("operation coverage changed: %d", count)
	}
}
