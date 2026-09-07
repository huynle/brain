package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

func TestGitRemotePushDispatchCompatibility(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		store := newFakeSchedulerStore()
		store.tasks = []types.ResolvedTask{{ID: "remote01", ProjectID: "p", Status: "pending", Classification: "ready", GitRemote: "https://supported.invalid/org/repo"}}
		store.runners = []types.RunnerInfo{
			{RunnerID: "a-wrong", Status: types.RunnerStatusOnline, DispatchPush: true, MaxParallel: 1, Capabilities: []string{"git-credential-host:other.invalid"}},
			{RunnerID: "b-offline", Status: types.RunnerStatusOffline, DispatchPush: true, MaxParallel: 1, Capabilities: []string{"git-credential-host:supported.invalid"}},
		}
		if compatible {
			store.runners = append(store.runners, types.RunnerInfo{RunnerID: "c-compatible", Status: types.RunnerStatusOnline, DispatchPush: true, MaxParallel: 1, Capabilities: []string{"git-credential-host:SUPPORTED.invalid:443"}})
		}
		svc := NewSchedulerService(store, nil, store)
		result, err := svc.ScheduleProject(context.Background(), "p")
		if err != nil {
			t.Fatal(err)
		}
		if !compatible && result.Dispatched != 0 {
			t.Fatalf("dispatched without compatible live runner: %+v", result)
		}
		if compatible && (len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "c-compatible") {
			t.Fatalf("wrong assigned runner: %+v", store.leases)
		}
	}
}

func TestGitRemotePullAndClaimCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		caps                  []string
		stale, unknown, allow bool
	}{
		{name: "unknown", unknown: true},
		{name: "no advertisement"},
		{name: "wrong host", caps: []string{"git-credential-host:other.invalid"}},
		{name: "stale", caps: []string{"git-credential-host:supported.invalid"}, stale: true},
		{name: "compatible", caps: []string{"git-credential-host:supported.invalid"}, allow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newTestTaskServiceWithDefaults(t, config.TaskDefaultsConfig{Executor: "opencode", ExecutionMode: "current_branch", TargetWorkdir: "/local/repo"})
			ctx := context.Background()
			insertTaskNote(t, store, "remote01", "Remote", "pending", "high", "p", map[string]interface{}{"git_remote": "https://supported.invalid/o/r"})
			if !tc.unknown {
				insertRunnerForTaskSelectionTest(t, store, "runner", []string{"opencode"}, tc.caps)
				if tc.stale {
					row, _ := store.GetRunner(ctx, "runner")
					row.LastHeartbeat = time.Now().Add(-time.Hour).UnixMilli()
					if err := store.UpsertRunner(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
			}
			next, err := svc.GetNext(ctx, "p", &api.TaskFilterOptions{RunnerID: "runner"})
			if err != nil {
				t.Fatal(err)
			}
			if (next != nil) != tc.allow {
				t.Errorf("pull returned %+v, allow=%v", next, tc.allow)
			}
			claim, err := svc.ClaimTask(ctx, "p", "remote01", "runner")
			if tc.allow {
				if err != nil || claim == nil || !claim.Success {
					t.Fatalf("compatible claim failed: %+v %v", claim, err)
				}
			} else {
				if err == nil {
					t.Errorf("incompatible direct claim accepted: %+v", claim)
				}
				var denial *types.PlacementDenialError
				if !errors.As(err, &denial) {
					t.Fatalf("expected typed placement denial, got %v", err)
				}
				wantReason := types.PlacementGitRemoteIneligible
				if tc.unknown {
					wantReason = types.PlacementRunnerUnregistered
				}
				reasons, historyErr := store.ListPlacementReasonRows(ctx, "p", "remote01")
				if historyErr != nil || len(reasons) != 1 || reasons[0].Reason != wantReason || denial.Reason != wantReason {
					t.Fatalf("denial=%+v history=%+v err=%v", denial, reasons, historyErr)
				}
				stored, _ := store.GetClaim(ctx, "p", "remote01")
				if stored != nil {
					t.Fatal("incompatible claim persisted")
				}
			}
		})
	}
}

func TestGitRemoteHeartbeatReplacesSupport(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	registry := NewRunnerRegistryService(store)
	ctx := context.Background()
	if _, err := registry.Register(ctx, types.RunnerRegistration{RunnerID: "r", Hostname: "r", Capabilities: []string{"git-credential-host:supported.invalid"}}); err != nil {
		t.Fatal(err)
	}
	for _, wire := range []string{`{"capabilities":["docker","git-credential-host:other.invalid"]}`, `{"capabilities":[]}`} {
		var req types.RunnerHeartbeatRequest
		if err := json.Unmarshal([]byte(wire), &req); err != nil {
			t.Fatal(err)
		}
		if err := registry.Heartbeat(ctx, "r", req); err != nil {
			t.Fatal(err)
		}
		_, err := brain.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "revoked", GitRemote: "https://supported.invalid/o/r"})
		if err == nil {
			t.Fatal("heartbeat retained revoked credential advertisement")
		}
		row, _ := store.GetRunner(ctx, "r")
		if wire == `{"capabilities":[]}` && len(row.Capabilities) != 0 {
			t.Fatalf("empty heartbeat did not clear capabilities: %v", row.Capabilities)
		}
	}
}

func TestGitRemotePausedOrDrainingRunnerCannotPullOrClaim(t *testing.T) {
	for _, draining := range []bool{false, true} {
		svc, store, _ := newTestTaskService(t)
		ctx := context.Background()
		insertTaskNote(t, store, "remote01", "Remote", "pending", "high", "p", map[string]interface{}{"git_remote": "https://supported.invalid/o/r"})
		insertRunnerForTaskSelectionTest(t, store, "r", nil, []string{"git-credential-host:supported.invalid"})
		if draining {
			row, _ := store.GetRunner(ctx, "r")
			row.Draining = true
			if err := store.UpsertRunner(ctx, row); err != nil {
				t.Fatal(err)
			}
		} else {
			registry := NewRunnerRegistryService(store)
			if err := registry.SetPaused(ctx, "r", true); err != nil {
				t.Fatal(err)
			}
		}
		next, err := svc.GetNext(ctx, "p", &api.TaskFilterOptions{RunnerID: "r"})
		if err != nil {
			t.Fatal(err)
		}
		if next != nil {
			t.Error("paused/draining runner received remote task")
		}
		if _, err := svc.ClaimTask(ctx, "p", "remote01", "r"); err == nil {
			t.Error("paused/draining runner claimed remote task")
		}
	}
}
