package storage

import (
	"context"
	"testing"
)

func TestDispatchLeaseSchema_FreshDB(t *testing.T) {
	s := newTestStorage(t)

	var name string
	if err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='task_dispatch_leases'",
	).Scan(&name); err != nil {
		t.Fatalf("task_dispatch_leases table not found: %v", err)
	}

	_, err := s.DB().Exec(`INSERT INTO task_dispatch_leases (
		project_id, task_id, assigned_runner_id, assigned_machine_id, state,
		pushed_at, acked_at, rejected_at, last_error, expires_at
	) VALUES ('brain-api', 'task-1', 'runner-1', 'machine-1', 'pushed', 1000, 0, 0, '', 2000)`)
	if err != nil {
		t.Fatalf("insert into task_dispatch_leases failed: %v", err)
	}

	indexes := []string{
		"idx_task_dispatch_leases_runner",
		"idx_task_dispatch_leases_machine",
		"idx_task_dispatch_leases_state",
		"idx_task_dispatch_leases_expires",
	}
	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var idxName string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
			).Scan(&idxName)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx, err)
			}
		})
	}
}

func TestDispatchLeaseOperations_AreAtomicAndPersistState(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	lease, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{
		ProjectID:         "brain-api",
		TaskID:            "task-1",
		AssignedRunnerID:  "runner-1",
		AssignedMachineID: "machine-1",
		PushedAt:          1000,
		ExpiresAt:         2000,
	})
	if err != nil {
		t.Fatalf("CreateDispatchLease failed: %v", err)
	}
	if !created {
		t.Fatal("expected lease to be created")
	}
	if lease.State != DispatchLeaseStatePushed || lease.AssignedRunnerID != "runner-1" || lease.AssignedMachineID != "machine-1" {
		t.Fatalf("created lease = %#v", lease)
	}
	if lease.LeaseID == "" {
		t.Fatalf("created lease LeaseID is empty: %#v", lease)
	}

	_, created, err = s.CreateDispatchLease(ctx, DispatchLeaseCreate{
		ProjectID:         "brain-api",
		TaskID:            "task-1",
		AssignedRunnerID:  "runner-2",
		AssignedMachineID: "machine-2",
		PushedAt:          1100,
		ExpiresAt:         2100,
	})
	if err != nil {
		t.Fatalf("duplicate active CreateDispatchLease failed: %v", err)
	}
	if created {
		t.Fatal("expected duplicate active lease not to be created")
	}

	acked, err := s.AckDispatchLease(ctx, "brain-api", "task-1", "runner-2", lease.LeaseID, 1200)
	if err != nil {
		t.Fatalf("AckDispatchLease wrong runner failed: %v", err)
	}
	if acked {
		t.Fatal("expected ack by wrong runner to be rejected")
	}

	acked, err = s.AckDispatchLease(ctx, "brain-api", "task-1", "runner-1", lease.LeaseID, 1300)
	if err != nil {
		t.Fatalf("AckDispatchLease failed: %v", err)
	}
	if !acked {
		t.Fatal("expected ack to update pushed lease")
	}

	got, err := s.GetDispatchLeaseRow(ctx, "brain-api", "task-1")
	if err != nil {
		t.Fatalf("GetDispatchLease failed: %v", err)
	}
	if got == nil || got.State != DispatchLeaseStateAcked || got.AckedAt != 1300 || got.PushedAt != 1000 || got.ExpiresAt != 2000 {
		t.Fatalf("acked lease = %#v", got)
	}

	rejected, err := s.RejectDispatchLease(ctx, "brain-api", "task-1", "runner-1", lease.LeaseID, 1400, "no capacity")
	if err != nil {
		t.Fatalf("RejectDispatchLease on acked failed: %v", err)
	}
	if rejected {
		t.Fatal("expected reject not to change acked lease")
	}

	released, err := s.ReleaseDispatchLease(ctx, "brain-api", "task-1", "runner-2")
	if err != nil {
		t.Fatalf("ReleaseDispatchLease wrong runner failed: %v", err)
	}
	if released {
		t.Fatal("expected release by wrong runner to be rejected")
	}

	released, err = s.ReleaseDispatchLease(ctx, "brain-api", "task-1", "runner-1")
	if err != nil {
		t.Fatalf("ReleaseDispatchLease failed: %v", err)
	}
	if !released {
		t.Fatal("expected owned lease to be released")
	}
	got, err = s.GetDispatchLeaseRow(ctx, "brain-api", "task-1")
	if err != nil {
		t.Fatalf("GetDispatchLease after release failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected released lease to be gone, got %#v", got)
	}
}

func TestDispatchLeaseRejectAndExpire(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	lease, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "task-2", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 1500})
	if err != nil || !created {
		t.Fatalf("create reject target: created=%v err=%v", created, err)
	}
	rejected, err := s.RejectDispatchLease(ctx, "brain-api", "task-2", "runner-1", lease.LeaseID, 1200, "agent unavailable")
	if err != nil {
		t.Fatalf("RejectDispatchLease failed: %v", err)
	}
	if !rejected {
		t.Fatal("expected pushed lease to be rejected")
	}
	got, err := s.GetDispatchLeaseRow(ctx, "brain-api", "task-2")
	if err != nil {
		t.Fatalf("GetDispatchLease failed: %v", err)
	}
	if got.State != DispatchLeaseStateRejected || got.RejectedAt != 1200 || got.LastError != "agent unavailable" {
		t.Fatalf("rejected lease = %#v", got)
	}

	if _, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "old", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 1100}); err != nil || !created {
		t.Fatalf("create old lease: created=%v err=%v", created, err)
	}
	if _, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "fresh", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 3000}); err != nil || !created {
		t.Fatalf("create fresh lease: created=%v err=%v", created, err)
	}

	expired, err := s.ExpireDispatchLeases(ctx, 2000)
	if err != nil {
		t.Fatalf("ExpireDispatchLeases failed: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired count = %d, want 1", expired)
	}
	old, err := s.GetDispatchLeaseRow(ctx, "brain-api", "old")
	if err != nil {
		t.Fatalf("get expired lease: %v", err)
	}
	if old == nil || old.State != DispatchLeaseStateExpired {
		t.Fatalf("old lease after expiry = %#v, want expired row retained", old)
	}
	fresh, err := s.GetDispatchLeaseRow(ctx, "brain-api", "fresh")
	if err != nil {
		t.Fatalf("get fresh lease: %v", err)
	}
	if fresh == nil || fresh.State != DispatchLeaseStatePushed {
		t.Fatalf("fresh lease after expiry = %#v, want pushed", fresh)
	}
}

func TestDispatchLeaseExpiredCommandsAreIgnoredAndRedispatchable(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	lease, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{
		ProjectID:         "brain-api",
		TaskID:            "stale-task",
		AssignedRunnerID:  "runner-old",
		AssignedMachineID: "machine-old",
		PushedAt:          1000,
		ExpiresAt:         1500,
	})
	if err != nil || !created {
		t.Fatalf("CreateDispatchLease stale target: created=%v err=%v", created, err)
	}

	acked, err := s.AckDispatchLease(ctx, "brain-api", "stale-task", "runner-old", lease.LeaseID, 1600)
	if err != nil {
		t.Fatalf("AckDispatchLease after expiry failed: %v", err)
	}
	if acked {
		t.Fatal("expected ack after lease expiry to be ignored")
	}

	rejected, err := s.RejectDispatchLease(ctx, "brain-api", "stale-task", "runner-old", lease.LeaseID, 1700, "stale command")
	if err != nil {
		t.Fatalf("RejectDispatchLease after expiry failed: %v", err)
	}
	if rejected {
		t.Fatal("expected reject after lease expiry to be ignored")
	}

	expired, err := s.ExpireDispatchLeases(ctx, 1800)
	if err != nil {
		t.Fatalf("ExpireDispatchLeases failed: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired count = %d, want 1", expired)
	}

	old, err := s.GetDispatchLeaseRow(ctx, "brain-api", "stale-task")
	if err != nil {
		t.Fatalf("GetDispatchLease expired lease: %v", err)
	}
	if old == nil || old.State != DispatchLeaseStateExpired || old.AssignedRunnerID != "runner-old" {
		t.Fatalf("expired lease = %#v, want old runner retained in expired state", old)
	}

	lease, created, err = s.CreateDispatchLease(ctx, DispatchLeaseCreate{
		ProjectID:         "brain-api",
		TaskID:            "stale-task",
		AssignedRunnerID:  "runner-new",
		AssignedMachineID: "machine-new",
		PushedAt:          1900,
		ExpiresAt:         2500,
	})
	if err != nil || !created {
		t.Fatalf("CreateDispatchLease redispatch: created=%v err=%v", created, err)
	}
	if lease.State != DispatchLeaseStatePushed || lease.AssignedRunnerID != "runner-new" || lease.AssignedMachineID != "machine-new" || lease.PushedAt != 1900 || lease.ExpiresAt != 2500 {
		t.Fatalf("redispatched lease = %#v", lease)
	}
}

func TestDispatchLeaseAckRejectRequireMatchingLeaseID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	lease, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "lease-bound-task", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 2000})
	if err != nil || !created {
		t.Fatalf("CreateDispatchLease: created=%v err=%v", created, err)
	}
	if lease.LeaseID == "" {
		t.Fatal("expected generated lease ID")
	}
	acked, err := s.AckDispatchLease(ctx, "brain-api", "lease-bound-task", "runner-1", "wrong-lease", 1100)
	if err != nil {
		t.Fatalf("AckDispatchLease wrong lease ID: %v", err)
	}
	if acked {
		t.Fatal("expected ack with mismatched lease ID to be rejected")
	}
	rejected, err := s.RejectDispatchLease(ctx, "brain-api", "lease-bound-task", "runner-1", "wrong-lease", 1200, "stale")
	if err != nil {
		t.Fatalf("RejectDispatchLease wrong lease ID: %v", err)
	}
	if rejected {
		t.Fatal("expected reject with mismatched lease ID to be rejected")
	}
	acked, err = s.AckDispatchLease(ctx, "brain-api", "lease-bound-task", "runner-1", lease.LeaseID, 1300)
	if err != nil {
		t.Fatalf("AckDispatchLease matching lease ID: %v", err)
	}
	if !acked {
		t.Fatal("expected ack with matching lease ID to succeed")
	}
}

func TestDispatchLeaseRedispatchUsesDifferentLeaseID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	first, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "redispatch-task", AssignedRunnerID: "runner-old", AssignedMachineID: "machine-old", PushedAt: 1000, ExpiresAt: 1500})
	if err != nil || !created {
		t.Fatalf("CreateDispatchLease first: created=%v err=%v", created, err)
	}
	if first.LeaseID == "" {
		t.Fatal("expected first lease ID")
	}
	if _, err := s.ExpireDispatchLeases(ctx, 1600); err != nil {
		t.Fatalf("ExpireDispatchLeases: %v", err)
	}
	second, created, err := s.CreateDispatchLease(ctx, DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "redispatch-task", AssignedRunnerID: "runner-new", AssignedMachineID: "machine-new", PushedAt: 1700, ExpiresAt: 2500})
	if err != nil || !created {
		t.Fatalf("CreateDispatchLease second: created=%v err=%v", created, err)
	}
	if second.LeaseID == "" {
		t.Fatal("expected second lease ID")
	}
	if second.LeaseID == first.LeaseID {
		t.Fatalf("redispatch reused lease ID %q", second.LeaseID)
	}
	acked, err := s.AckDispatchLease(ctx, "brain-api", "redispatch-task", "runner-new", first.LeaseID, 1800)
	if err != nil {
		t.Fatalf("AckDispatchLease stale lease ID: %v", err)
	}
	if acked {
		t.Fatal("expected stale lease ID to be rejected after redispatch")
	}
	rejected, err := s.RejectDispatchLease(ctx, "brain-api", "redispatch-task", "runner-new", first.LeaseID, 1900, "stale")
	if err != nil {
		t.Fatalf("RejectDispatchLease stale lease ID: %v", err)
	}
	if rejected {
		t.Fatal("expected stale lease ID reject to be ignored after redispatch")
	}
	acked, err = s.AckDispatchLease(ctx, "brain-api", "redispatch-task", "runner-new", second.LeaseID, 2000)
	if err != nil {
		t.Fatalf("AckDispatchLease fresh lease ID: %v", err)
	}
	if !acked {
		t.Fatal("expected fresh lease ID to ack redispatched lease")
	}
}

func TestPlacementReasons_AreQueryableSeparatelyFromTaskContent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.RecordPlacementReason(ctx, &PlacementReasonRow{
		ProjectID:      "brain-api",
		TaskID:         "task-1",
		RunnerID:       "runner-1",
		MachineID:      "machine-1",
		Decision:       "selected",
		Reason:         "preferred machine matched",
		RequiredLabels: `{"os":"darwin"}`,
		RunnerLabels:   `{"os":"darwin","zone":"local"}`,
		MissingLabels:  `[]`,
		CreatedAt:      1000,
	}); err != nil {
		t.Fatalf("RecordPlacementReason selected failed: %v", err)
	}
	if err := s.RecordPlacementReason(ctx, &PlacementReasonRow{
		ProjectID:      "brain-api",
		TaskID:         "task-1",
		RunnerID:       "runner-2",
		MachineID:      "machine-2",
		Decision:       "rejected",
		Reason:         "missing required label",
		RequiredLabels: `{"os":"darwin"}`,
		RunnerLabels:   `{"os":"linux"}`,
		MissingLabels:  `["os"]`,
		CreatedAt:      1100,
	}); err != nil {
		t.Fatalf("RecordPlacementReason rejected failed: %v", err)
	}

	reasons, err := s.ListPlacementReasonRows(ctx, "brain-api", "task-1")
	if err != nil {
		t.Fatalf("ListPlacementReasons failed: %v", err)
	}
	if len(reasons) != 2 {
		t.Fatalf("len(reasons) = %d, want 2", len(reasons))
	}
	if reasons[0].Decision != "selected" || reasons[0].RunnerID != "runner-1" || reasons[0].Reason != "preferred machine matched" {
		t.Fatalf("first reason = %#v", reasons[0])
	}
	if reasons[1].Decision != "rejected" || reasons[1].RunnerID != "runner-2" || reasons[1].MissingLabels != `["os"]` {
		t.Fatalf("second reason = %#v", reasons[1])
	}
}

func TestDispatchLeaseAndPlacementReason_MigrationFromV18(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (18)"); err != nil {
		t.Fatalf("insert v18: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	for _, table := range []string{"task_dispatch_leases", "task_placement_reasons"} {
		t.Run(table, func(t *testing.T) {
			var name string
			if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
				t.Fatalf("table %s not found after migration: %v", table, err)
			}
		})
	}
	for _, idx := range []string{
		"idx_task_dispatch_leases_expires",
		"idx_task_dispatch_leases_state",
		"idx_task_placement_reasons_task",
		"idx_task_placement_reasons_created",
	} {
		t.Run(idx, func(t *testing.T) {
			var name string
			if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name); err != nil {
				t.Fatalf("index %s not found after migration: %v", idx, err)
			}
		})
	}
}

func TestListExpiredDispatchLeasesForReconciliation(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	cases := []DispatchLeaseCreate{
		{ProjectID: "brain-api", TaskID: "expired-pushed", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 1500},
		{ProjectID: "brain-api", TaskID: "fresh-pushed", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 3000},
		{ProjectID: "other", TaskID: "expired-other", AssignedRunnerID: "runner-1", AssignedMachineID: "machine-1", PushedAt: 1000, ExpiresAt: 1400},
	}
	for _, tc := range cases {
		if _, created, err := s.CreateDispatchLease(ctx, tc); err != nil || !created {
			t.Fatalf("CreateDispatchLease(%s/%s): created=%v err=%v", tc.ProjectID, tc.TaskID, created, err)
		}
	}

	expired, err := s.ListExpiredDispatchLeases(ctx, "brain-api", 2000, 10)
	if err != nil {
		t.Fatalf("ListExpiredDispatchLeases failed: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("len(expired) = %d, want 1: %#v", len(expired), expired)
	}
	if expired[0].TaskID != "expired-pushed" || expired[0].State != DispatchLeaseStatePushed {
		t.Fatalf("expired[0] = %#v", expired[0])
	}
}
