package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// These are API principal names, deliberately NOT runner IDs. Authentication
// supplies scope but has no trusted principal-to-runner binding yet (P7).
type placementTokenValidator struct{}

func (placementTokenValidator) ValidateToken(_ context.Context, token string) (*storage.Token, error) {
	switch token {
	case "operator":
		return &storage.Token{Name: "human-admin", Scope: "admin:*"}, nil
	case "runner-client":
		return &storage.Token{Name: "runner-api-client", Scope: "runner:*"}, nil
	default:
		return nil, fmt.Errorf("invalid test token")
	}
}

// External package avoids the service -> api import cycle while exercising the
// actual service, registry, persistence, event ingestion and SSE hub.
func TestTaskPlacementEndpoints(t *testing.T) {
	for _, endpoint := range []string{"claim", "dispatch"} {
		for _, tc := range []struct {
			name, affinity, origin, machine, remote, reason string
			unregistered, conflict                          bool
			caps                                            []string
		}{
			{name: "unregistered", unregistered: true, reason: "runner_unregistered"},
			{name: "machine mismatch", affinity: "local", origin: "home", machine: "away", reason: "machine_affinity_mismatch"},
			{name: "machine unresolved", affinity: "local", machine: "home", reason: "machine_affinity_unresolved"},
			{name: "missing capability", reason: "required_capability_missing"},
			{name: "git ineligible", caps: []string{"docker"}, remote: "https://github.com/org/repo.git", reason: "git_remote_ineligible"},
			{name: "legitimate local", affinity: "local", origin: "home", machine: "home", caps: []string{"docker"}},
			{name: "legitimate git", caps: []string{"docker", "git-credential-host:github.com"}, remote: "https://github.com/org/repo.git"},
			{name: "conflict", caps: []string{"docker"}, conflict: true},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				db, err := sql.Open("sqlite", ":memory:")
				if err != nil {
					t.Fatal(err)
				}
				store, err := storage.NewWithDB(db)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				dir := t.TempDir()
				svc := service.NewTaskService(&config.Config{BrainDir: dir}, store, indexer.NewIndexer(dir, store))
				meta, err := json.Marshal(map[string]any{"feature_id": "feature", "requires_capability": []string{"docker"}, "machine_affinity": tc.affinity, "origin_machine_id": tc.origin, "git_remote": tc.remote})
				if err != nil {
					t.Fatal(err)
				}
				typ, status, project := "task", "pending", "p"
				if _, err := store.InsertNote(ctx, &storage.NoteRow{Path: "projects/p/task/task0001.md", ShortID: "task0001", Title: "Placement", Type: &typ, Status: &status, ProjectID: &project, Metadata: string(meta)}); err != nil {
					t.Fatal(err)
				}
				if !tc.unregistered {
					now := time.Now().UnixMilli()
					if err := store.UpsertRunner(ctx, &storage.RunnerRow{RunnerID: "r", MachineID: tc.machine, Hostname: "host", Status: "online", RegisteredAt: now, LastHeartbeat: now, MaxParallel: 1, Capabilities: tc.caps}); err != nil {
						t.Fatal(err)
					}
				}
				if tc.conflict {
					if ok, _, err := store.ClaimTask(ctx, "p", "task0001", "owner", time.Hour); err != nil || !ok {
						t.Fatalf("seed claim: %v", err)
					}
				}
				hub := realtime.NewHub()
				commands, unsub := hub.Subscribe(realtime.RunnerTopic("r"))
				defer unsub()
				projectEvents, unsubProject := hub.Subscribe("p")
				defer unsubProject()
				events := service.NewEventService(realtime.NewEventHub())
				h := api.NewHandler(nil, api.WithTaskService(svc), api.WithRunnerRegistryService(service.NewRunnerRegistryService(store)), api.WithHub(hub), api.WithEventService(events))
				router := api.NewRouter(config.Config{EnableAuth: true}, api.WithHandler(h), api.WithTokenValidator(placementTokenValidator{}))
				field := "runnerId"
				token := "runner-client"
				if endpoint == "dispatch" {
					field = "targetRunnerId"
					token = "operator"
				}
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/p/task0001/"+endpoint, strings.NewReader(`{"`+field+`":"r"}`))
				req.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(rec, req)
				want := http.StatusOK
				if tc.reason != "" {
					want = http.StatusForbidden
				} else if tc.conflict {
					want = http.StatusConflict
				}
				if rec.Code != want {
					t.Errorf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
				}
				history, err := store.ListPlacementReasonRows(ctx, "p", "task0001")
				if err != nil {
					t.Fatal(err)
				}
				claim, err := store.GetClaim(ctx, "p", "task0001")
				if err != nil {
					t.Fatal(err)
				}
				lease, err := store.GetDispatchLeaseRow(ctx, "p", "task0001")
				if err != nil {
					t.Fatal(err)
				}
				recent, err := events.Recent(ctx, 100, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.reason != "" || tc.conflict {
					if tc.reason != "" {
						var body types.ErrorResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
							t.Fatal(err)
						}
						if body.Error != "Forbidden" || body.Message != tc.reason {
							t.Errorf("error envelope=%+v want Forbidden/%s", body, tc.reason)
						}
						if len(history) != 1 || history[0].Reason != tc.reason || history[0].RunnerID != "r" {
							t.Errorf("denial history=%+v", history)
						}
						if claim != nil {
							t.Errorf("denial created claim: %+v", claim)
						}
					} else if claim == nil || claim.RunnerID != "owner" {
						t.Errorf("conflict changed owner: %+v", claim)
					}
					if lease != nil {
						t.Errorf("refusal created lease: %+v", lease)
					}
					assignment, err := store.GetFeatureAssignment(ctx, "p", "feature")
					if err != nil || assignment != nil {
						t.Errorf("refusal assigned feature: %+v err=%v", assignment, err)
					}
					if len(recent) != 0 {
						t.Errorf("refusal emitted events: %+v", recent)
					}
					select {
					case msg := <-commands:
						t.Errorf("refusal dispatched: %+v", msg)
					default:
					}
					select {
					case msg := <-projectEvents:
						t.Errorf("refusal published: %+v", msg)
					default:
					}
					return
				}
				if len(history) != 0 {
					t.Errorf("success denial history=%+v", history)
				}
				if claim == nil || claim.RunnerID != "r" {
					t.Fatalf("missing legitimate claim: %+v", claim)
				}
				if endpoint == "dispatch" {
					var body types.DispatchResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if lease == nil || body.LeaseID == "" || body.LeaseID != lease.LeaseID || !body.Success {
						t.Fatalf("lease=%+v response=%+v", lease, body)
					}
					expires, err := time.Parse(time.RFC3339, body.ExpiresAt)
					if err != nil || !expires.After(time.Now()) || expires.After(time.Now().Add(61*time.Second)) {
						t.Errorf("dispatch expiry=%q err=%v", body.ExpiresAt, err)
					}
					select {
					case msg := <-commands:
						data := msg.Data.(map[string]interface{})
						payload := data["payload"].(map[string]string)
						if data["command"] != "dispatch" || payload["leaseId"] != body.LeaseID || payload["expiresAt"] != body.ExpiresAt {
							t.Errorf("command=%+v", data)
						}
					default:
						t.Fatal("no dispatch SSE")
					}
				} else {
					if lease != nil {
						t.Errorf("claim created dispatch lease: %+v", lease)
					}
					if len(recent) != 1 || recent[0].Type != types.EventTaskClaimed {
						t.Errorf("claim events=%+v", recent)
					}
					select {
					case <-projectEvents:
					default:
						t.Fatal("no claim SSE")
					}
				}
			})
		}
	}
}
