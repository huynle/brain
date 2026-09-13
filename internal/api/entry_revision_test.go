package api

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConditionalEntryConflictIsHTTP409(t *testing.T) {
	brain := &mockBrainService{updateFunc: func(context.Context, string, types.UpdateEntryRequest) (*types.BrainEntry, error) {
		return nil, fmt.Errorf("%w: revision changed", ErrConflict)
	}, updateMetadataFunc: func(context.Context, string, map[string]interface{}) (*types.BrainEntry, error) {
		return nil, fmt.Errorf("%w: revision changed", ErrConflict)
	}}
	h := &Handler{brain: brain}
	router := chi.NewRouter()
	router.Patch("/entries/{id}", h.HandleUpdateEntry)
	router.Patch("/entries/{id}/metadata", h.HandleUpdateMetadata)
	for _, path := range []string{"/entries/task", "/entries/task/metadata"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("PATCH", path, strings.NewReader(`{"expected_revision":"stale","status":"completed"}`)))
		if w.Code != 409 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
}
