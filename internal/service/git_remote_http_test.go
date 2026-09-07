package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/types"
)

// Exercise real HTTP handlers, services, SQLite and markdown persistence, not
// mocked service errors. No running Brain server or remote Git host is used.
func TestGitRemoteHTTPAdmission(t *testing.T) {
	for _, operation := range []string{"create", "update", "metadata", "checkout"} {
		t.Run(operation, func(t *testing.T) {
			svc, store, dir := newTestBrainService(t)
			tasks := NewTaskService(&config.Config{BrainDir: dir}, store, indexer.NewIndexer(dir, store))
			h := api.NewHandler(svc, api.WithTaskService(tasks))
			router := chi.NewRouter()
			router.Post("/entries", h.HandleCreateEntry)
			router.Patch("/entries/*", h.HandleUpdateOrMetadata)
			router.Post("/tasks/{projectId}/features/{featureId}/checkout", h.HandleCheckoutFeature)
			server := httptest.NewServer(router)
			defer server.Close()

			method, path := http.MethodPost, "/entries"
			body := `{"type":"task","title":"remote task","content":"test","project":"p","git_remote":"https://unsupported.invalid/o/r"}`
			if operation == "update" || operation == "metadata" {
				saved, err := svc.Save(context.Background(), types.CreateEntryRequest{Type: "task", Title: "original", Project: "p"})
				if err != nil {
					t.Fatal(err)
				}
				method, path = http.MethodPatch, "/entries/"+saved.ID
				body = `{"git_remote":"https://unsupported.invalid/o/r"}`
				if operation == "metadata" {
					path += "/metadata"
					body = `{"status":"pending"}`
					if _, err := store.MergeMetadata(context.Background(), saved.Path, map[string]interface{}{"git_remote": "https://unsupported.invalid/o/r"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if operation == "checkout" {
				taskDir := filepath.Join(dir, "projects", "p", "task")
				if err := os.MkdirAll(taskDir, 0755); err != nil {
					t.Fatal(err)
				}
				// A legacy feature must also be refused before checkout writes.
				if err := os.WriteFile(filepath.Join(taskDir, "feature1.md"), []byte("---\ntype: task\ntitle: Feature\nstatus: completed\nfeature_id: feature\ngit_remote: https://unsupported.invalid/o/r\n---\n"), 0644); err != nil {
					t.Fatal(err)
				}
				path, body = "/tasks/p/features/feature/checkout", `{}`
			}
			request := func(body string) (int, string) {
				t.Helper()
				req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				return resp.StatusCode, string(data)
			}
			status, response := request(body)
			if status != http.StatusBadRequest || !strings.Contains(response, "unsupported.invalid") {
				t.Errorf("unsupported host: status=%d body=%s; want 400 naming host", status, response)
			}
			if operation == "create" {
				insertRunnerForTaskSelectionTest(t, store, "configured", nil, []string{"git-credential-host:supported.invalid"})
				status, response = request(strings.ReplaceAll(body, "unsupported.invalid", "supported.invalid"))
				if status != http.StatusCreated {
					t.Fatalf("registered host: status=%d body=%s", status, response)
				}
				var saved types.CreateEntryResponse
				if err := json.Unmarshal([]byte(response), &saved); err != nil {
					t.Fatal(err)
				}
				entry, err := svc.Recall(context.Background(), saved.ID)
				if err != nil || entry.GitRemote != "https://supported.invalid/o/r" {
					t.Fatalf("registered host not persisted: %+v, %v", entry, err)
				}
			}
		})
	}
}
