package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func TestClaimPlacementEligibility(t *testing.T) {
	for _, method := range []string{"claim", "duration", "dispatch"} {
		for _, tc := range []struct {
			name, affinity, origin, machine, reason string
			unknown, missing, legacy                bool
			caps                                    []string
		}{
			{name: "unregistered", unknown: true, reason: "runner_unregistered"},
			{name: "unregistered missing task", unknown: true, missing: true, reason: "runner_unregistered"},
			{name: "local mismatch", affinity: "local", origin: "home", machine: "away", caps: []string{"docker"}, reason: "machine_affinity_mismatch"},
			{name: "local unknown machine", affinity: "local", origin: "home", reason: "machine_affinity_mismatch"},
			{name: "local unresolved", affinity: "local", machine: "home", reason: "machine_affinity_unresolved"},
			{name: "capability short", caps: []string{"other"}, reason: "required_capability_missing"},
			{name: "valid local", affinity: "local", origin: "home", machine: "home", caps: []string{"docker"}},
			{name: "preferred", affinity: "preferred", origin: "home", machine: "away", caps: []string{"docker"}},
			{name: "none", affinity: "none", origin: "home", machine: "away", caps: []string{"docker"}},
			{name: "legacy machine label", affinity: "local", origin: "home", machine: "home", legacy: true, caps: []string{"docker"}},
			{name: "registered missing task", missing: true},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				svc, store, _ := newTestTaskService(t)
				ctx := context.Background()
				if !tc.missing {
					insertTaskNote(t, store, "task0001", "Task", "pending", "high", "p", map[string]interface{}{
						"feature_id": "feature", "machine_affinity": tc.affinity, "origin_machine_id": tc.origin, "requires_capability": []string{"docker"},
					})
				}
				if !tc.unknown {
					insertRunnerForTaskSelectionTest(t, store, "r", nil, tc.caps)
					row, err := store.GetRunner(ctx, "r")
					if err != nil {
						t.Fatal(err)
					}
					row.MachineID = tc.machine
					if tc.legacy {
						row.MachineID = ""
						row.Labels[machineIDLabel] = tc.machine
					}
					if err := store.UpsertRunner(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch method {
				case "claim":
					_, err = svc.ClaimTask(ctx, "p", "task0001", "r")
				case "duration":
					_, err = svc.ClaimTaskWithDuration(ctx, "p", "task0001", "r", time.Minute)
				case "dispatch":
					_, err = svc.DispatchTask(ctx, "p", "task0001", "r")
				}
				if tc.reason == "" {
					if err != nil {
						t.Fatal(err)
					}
					claim, err := store.GetClaim(ctx, "p", "task0001")
					if err != nil || claim == nil || claim.RunnerID != "r" {
						t.Fatalf("claim=%+v err=%v", claim, err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), tc.reason) {
					t.Fatalf("want denial %s, got %v", tc.reason, err)
				}
				var denial *types.PlacementDenialError
				if !errors.As(err, &denial) || denial.Reason != tc.reason {
					t.Fatalf("want typed denial %s, got %T: %v", tc.reason, err, err)
				}
				reasons, err := store.ListPlacementReasonRows(ctx, "p", "task0001")
				if err != nil || len(reasons) != 1 {
					t.Fatalf("history=%+v err=%v", reasons, err)
				}
				if reasons[0].Reason != tc.reason || reasons[0].RunnerID != "r" || reasons[0].MachineID != tc.machine {
					t.Fatalf("wrong history: %+v", reasons[0])
				}
				assertClaimPlacementNoOwnership(t, store)
			})
		}
	}
}

func assertClaimPlacementNoOwnership(t *testing.T, store *storage.StorageLayer) {
	t.Helper()
	ctx := context.Background()
	claim, err := store.GetClaim(ctx, "p", "task0001")
	if err != nil || claim != nil {
		t.Fatalf("unexpected claim=%+v err=%v", claim, err)
	}
	lease, err := store.GetDispatchLeaseRow(ctx, "p", "task0001")
	if err != nil || lease != nil {
		t.Fatalf("unexpected lease=%+v err=%v", lease, err)
	}
	assignment, err := store.GetFeatureAssignment(ctx, "p", "feature")
	if err != nil || assignment != nil {
		t.Fatalf("unexpected assignment=%+v err=%v", assignment, err)
	}
}

func TestClaimPlacementDenialPreservesOwnershipAndBoundsHistory(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	insertTaskNote(t, store, "task0001", "Task", "pending", "high", "p", map[string]interface{}{"feature_id": "feature"})
	if ok, _, err := store.ClaimTask(ctx, "p", "task0001", "owner", time.Hour); err != nil || !ok {
		t.Fatalf("seed claim: %v", err)
	}
	claim, _ := store.GetClaim(ctx, "p", "task0001")
	lease, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{ProjectID: "p", TaskID: "task0001", AssignedRunnerID: "owner", PushedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("seed lease: %v", err)
	}
	for i := 0; i < storage.PlacementReasonRetention+3; i++ {
		if _, err := svc.DispatchTask(ctx, "p", "task0001", "stranger"); err == nil || !strings.Contains(err.Error(), "runner_unregistered") {
			t.Fatalf("expected placement denial, got %v", err)
		}
	}
	reasons, err := store.ListPlacementReasonRows(ctx, "p", "task0001")
	if err != nil || len(reasons) != storage.PlacementReasonRetention {
		t.Fatalf("history count=%d err=%v", len(reasons), err)
	}
	afterClaim, _ := store.GetClaim(ctx, "p", "task0001")
	afterLease, _ := store.GetDispatchLeaseRow(ctx, "p", "task0001")
	if !reflect.DeepEqual(claim, afterClaim) || !reflect.DeepEqual(lease, afterLease) {
		t.Fatal("denial changed competing ownership")
	}
	assignment, err := store.GetFeatureAssignment(ctx, "p", "feature")
	if err != nil || assignment != nil {
		t.Fatalf("denial assigned feature: %+v %v", assignment, err)
	}
}

func TestClaimPlacementStorageFailuresFailClosed(t *testing.T) {
	for _, table := range []string{"runners", "notes", "task_placement_reasons"} {
		t.Run(table, func(t *testing.T) {
			svc, store, _ := newTestTaskService(t)
			ctx := context.Background()
			insertTaskNote(t, store, "task0001", "Task", "pending", "high", "p", map[string]interface{}{"feature_id": "feature"})
			if table != "task_placement_reasons" {
				insertRunnerForTaskSelectionTest(t, store, "r", nil, nil)
			}
			if _, err := store.DB().ExecContext(ctx, "DROP TABLE "+table); err != nil {
				t.Fatal(err)
			}
			_, err := svc.DispatchTask(ctx, "p", "task0001", "r")
			if err == nil {
				t.Fatal("storage failure permitted dispatch")
			}
			if table == "task_placement_reasons" {
				var denial *types.PlacementDenialError
				if !errors.As(err, &denial) || denial.Reason != types.PlacementRunnerUnregistered || !strings.Contains(err.Error(), "record claim denial") {
					t.Fatalf("history failure lost denial or persistence error: %v", err)
				}
			}
			assertClaimPlacementNoOwnership(t, store)
		})
	}
}
