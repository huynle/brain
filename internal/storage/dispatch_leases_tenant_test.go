package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantDispatchLifecycle(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	c := context.Background()
	create := func(s *TenantStore, task, lease string, pushed, expires int64) (*DispatchLeaseRow, bool) {
		t.Helper()
		r, ok, err := s.CreateDispatchLease(c, DispatchLeaseCreate{ProjectID: "shared", TaskID: task, LeaseID: lease, AssignedRunnerID: "same", PushedAt: pushed, ExpiresAt: expires})
		if err != nil {
			t.Fatalf("create tenant %s: %v", s.TenantID(), err)
		}
		return r, ok
	}
	for _, s := range []*TenantStore{a, b} {
		if err := s.UpsertRunner(c, &RunnerRow{RunnerID: "same"}); err != nil {
			t.Fatal(err)
		}
		for _, task := range []string{"ack", "reject", "expire", "clear", "release"} {
			if _, ok := create(s, task, "same-lease", 10, 20); !ok {
				t.Fatal("create lost")
			}
		}
	}
	before, err := b.GetDispatchLeaseRow(c, "shared", "ack")
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := create(a, "ack", "replacement", 15, 30); ok || r.LeaseID != "same-lease" {
		t.Fatalf("active overwrite: %+v %v", r, ok)
	}
	for _, tc := range []struct {
		runner, lease string
		at            int64
	}{{"wrong", "same-lease", 15}, {"same", "wrong", 15}, {"same", "same-lease", 21}} {
		if ok, e := a.AckDispatchLease(c, "shared", "ack", tc.runner, tc.lease, tc.at); e != nil || ok {
			t.Fatalf("invalid ack: %v %v", ok, e)
		}
		if ok, e := a.RejectDispatchLease(c, "shared", "reject", tc.runner, tc.lease, tc.at, "bad"); e != nil || ok {
			t.Fatalf("invalid reject: %v %v", ok, e)
		}
	}
	if ok, e := a.AckDispatchLease(c, "shared", "ack", "same", "same-lease", 20); e != nil || !ok {
		t.Fatalf("ack boundary: %v %v", ok, e)
	}
	if ok, e := a.AckDispatchLease(c, "shared", "ack", "same", "same-lease", 20); e != nil || ok {
		t.Fatalf("repeated ack: %v %v", ok, e)
	}
	if ok, e := a.RejectDispatchLease(c, "shared", "ack", "same", "same-lease", 20, "bad"); e != nil || ok {
		t.Fatalf("reject acked: %v %v", ok, e)
	}
	if ok, e := a.RejectDispatchLease(c, "shared", "reject", "same", "same-lease", 20, "bad"); e != nil || !ok {
		t.Fatalf("reject: %v %v", ok, e)
	}
	if ok, e := a.ReleaseDispatchLease(c, "shared", "release", "wrong"); e != nil || ok {
		t.Fatalf("wrong release: %v %v", ok, e)
	}
	if ok, e := a.ReleaseDispatchLease(c, "shared", "release", "same"); e != nil || !ok {
		t.Fatalf("release: %v %v", ok, e)
	}
	if ok, e := a.ClearDispatchLease(c, "shared", "clear"); e != nil || !ok {
		t.Fatalf("clear: %v %v", ok, e)
	}
	if ok, e := a.ClearDispatchLease(c, "shared", "clear"); e != nil || ok {
		t.Fatalf("clear missing: %v %v", ok, e)
	}
	if rs, e := a.ListExpiredDispatchLeases(c, "shared", 20, 0); e != nil || len(rs) != 0 {
		t.Fatalf("expiry boundary: %+v %v", rs, e)
	}
	create(a, "later", "later", 11, 21)
	if rs, e := a.ListExpiredDispatchLeases(c, "shared", 22, 2); e != nil || len(rs) != 2 || rs[0].ExpiresAt != 20 || rs[1].ExpiresAt != 20 {
		t.Fatalf("expiry limit/order: %+v %v", rs, e)
	}
	if n, e := a.ExpireDispatchLeases(c, 21); e != nil || n != 1 {
		t.Fatalf("expire: %d %v", n, e)
	}
	if r, e := a.GetDispatchLease(c, "shared", "ack"); e != nil || r.State != DispatchLeaseStateAcked || r.AckedAt != 20 || r.ID != r.LeaseID {
		t.Fatalf("acked retained: %+v %v", r, e)
	}
	if r, e := a.GetDispatchLeaseRow(c, "shared", "expire"); e != nil || r.State != DispatchLeaseStateExpired || r.LastError != "dispatch lease expired" {
		t.Fatalf("expired: %+v %v", r, e)
	}
	for _, task := range []string{"ack", "reject", "expire"} {
		r, ok := create(a, task, "new", 22, 40)
		if !ok || r.State != DispatchLeaseStatePushed || r.AckedAt != 0 || r.RejectedAt != 0 || r.LastError != "" {
			t.Fatalf("replace: %+v %v", r, ok)
		}
		if ok, e := a.AckDispatchLease(c, "shared", task, "same", "same-lease", 23); e != nil || ok {
			t.Fatalf("stale fence: %v %v", ok, e)
		}
	}
	if r, e := b.GetDispatchLeaseRow(c, "shared", "ack"); e != nil || !reflect.DeepEqual(r, before) {
		t.Fatalf("foreign changed: %+v %v", r, e)
	}
	for _, task := range []string{"reject", "expire", "clear", "release"} {
		if r, e := b.GetDispatchLeaseRow(c, "shared", task); e != nil || r == nil || r.State != DispatchLeaseStatePushed {
			t.Fatalf("foreign %s changed: %+v %v", task, r, e)
		}
	}
}

func TestTenantDispatchReasons(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	c := context.Background()
	for i := 0; i < 25; i++ {
		for _, s := range []*TenantStore{a, b} {
			// Empty runner is the established no-candidate sentinel.
			if e := s.RecordPlacementReason(c, &PlacementReasonRow{ProjectID: "shared", TaskID: "same", Decision: "no_candidate", Reason: fmt.Sprint(i), CreatedAt: int64(i / 2)}); e != nil {
				t.Fatal(e)
			}
		}
	}
	before, e := b.ListPlacementReasonRows(c, "shared", "same")
	if e != nil || len(before) != 20 {
		t.Fatalf("retention: %d %v", len(before), e)
	}
	for _, limit := range []int{-1, 0, 1, 3, 50} {
		rs, e := a.ListPlacementReasonsLimit(c, "shared", "same", limit)
		n := 20
		if limit > 0 && limit < n {
			n = limit
		}
		if e != nil || len(rs) != n {
			t.Fatalf("limit %d: %d %v", limit, len(rs), e)
		}
		for i, r := range rs {
			if r.Reason != fmt.Sprint(25-n+i) {
				t.Fatalf("order: %+v", rs)
			}
		}
	}
	if rs, e := a.ListPlacementReasons(c, "shared", "same"); e != nil || len(rs) != 20 {
		t.Fatalf("wrapper: %d %v", len(rs), e)
	}
	if n, e := a.PrunePlacementReasonsForTask(c, "shared", "same", 0); e != nil || n != 0 {
		t.Fatalf("noop prune: %d %v", n, e)
	}
	if n, e := a.PrunePlacementReasonsForTask(c, "shared", "same", 3); e != nil || n != 17 {
		t.Fatalf("prune: %d %v", n, e)
	}
	if rs, e := a.ListPlacementReasonRows(c, "shared", "same"); e != nil || len(rs) != 3 || rs[0].Reason != "22" {
		t.Fatalf("pruned: %+v %v", rs, e)
	}
	if rs, e := b.ListPlacementReasonRows(c, "shared", "same"); e != nil || !reflect.DeepEqual(rs, before) {
		t.Fatalf("foreign prune: %+v %v", rs, e)
	}
}

func TestTenantDispatchReferences(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	c := context.Background()
	if e := b.UpsertRunner(c, &RunnerRow{RunnerID: "foreign"}); e != nil {
		t.Fatal(e)
	}
	if e := a.UpsertRunner(c, &RunnerRow{RunnerID: "same"}); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := a.CreateDispatchLease(c, DispatchLeaseCreate{ProjectID: "shared", TaskID: "occupied", AssignedRunnerID: "same", ExpiresAt: 1}); e != nil || !ok {
		t.Fatalf("seed: %v %v", ok, e)
	}
	for _, task := range []string{"new", "occupied"} {
		if _, ok, e := a.CreateDispatchLease(c, DispatchLeaseCreate{ProjectID: "shared", TaskID: task, AssignedRunnerID: "foreign", PushedAt: 2, ExpiresAt: 3}); e == nil || ok {
			t.Fatalf("foreign lease accepted: %v %v", ok, e)
		}
	}
	if e := a.RecordPlacementReason(c, &PlacementReasonRow{ProjectID: "shared", TaskID: "same", RunnerID: "foreign"}); e == nil {
		t.Fatal("foreign reason accepted")
	}
	var n int
	if e := owner.db.QueryRow("SELECT count(*) FROM tenant_runner_keys WHERE tenant_id='local' AND runner_id='foreign'").Scan(&n); e != nil || n != 0 {
		t.Fatalf("manufactured reference: %d %v", n, e)
	}
	if _, e := b.DeleteRunner(c, "foreign"); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := b.CreateDispatchLease(c, DispatchLeaseCreate{ProjectID: "shared", TaskID: "durable", AssignedRunnerID: "foreign"}); e != nil || !ok {
		t.Fatalf("durable lease: %v %v", ok, e)
	}
	if e := b.RecordPlacementReason(c, &PlacementReasonRow{ProjectID: "shared", TaskID: "same", RunnerID: "foreign"}); e != nil {
		t.Fatal(e)
	}
}

func TestTenantDispatchScopeGuards(t *testing.T) {
	ops := []func(*TenantStore, context.Context) error{
		func(s *TenantStore, c context.Context) error {
			_, _, e := s.CreateDispatchLease(c, DispatchLeaseCreate{})
			return e
		},
		func(s *TenantStore, c context.Context) error { _, e := s.GetDispatchLeaseRow(c, "p", "t"); return e },
		func(s *TenantStore, c context.Context) error {
			_, e := s.AckDispatchLease(c, "p", "t", "r", "l", 1)
			return e
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.RejectDispatchLease(c, "p", "t", "r", "l", 1, "")
			return e
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.ReleaseDispatchLease(c, "p", "t", "r")
			return e
		},
		func(s *TenantStore, c context.Context) error { _, e := s.ClearDispatchLease(c, "p", "t"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.ExpireDispatchLeases(c, 1); return e },
		func(s *TenantStore, c context.Context) error {
			return s.RecordPlacementReason(c, &PlacementReasonRow{})
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.ListPlacementReasonRows(c, "p", "t")
			return e
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.ListPlacementReasonRowsLimit(c, "p", "t", 1)
			return e
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.PrunePlacementReasonsForTask(c, "p", "t", 0)
			return e
		},
		func(s *TenantStore, c context.Context) error { _, e := s.GetDispatchLease(c, "p", "t"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.ListPlacementReasons(c, "p", "t"); return e },
		func(s *TenantStore, c context.Context) error {
			_, e := s.ListPlacementReasonsLimit(c, "p", "t", 0)
			return e
		},
		func(s *TenantStore, c context.Context) error {
			_, e := s.ListExpiredDispatchLeases(c, "p", 1, 0)
			return e
		},
	}
	for i, op := range ops {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			owner := newTestStorage(t)
			local, _ := owner.ForTenant(tenant.Local)
			foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, tc := range []struct {
				name string
				s    *TenantStore
				c    context.Context
			}{
				{"foreign28", foreign, context.Background()}, {"nil", nil, context.Background()}, {"zero", &TenantStore{}, context.Background()}, {"invalidID", &TenantStore{db: owner.db}, context.Background()}, {"cancelled", local, cancelled}, {"nilContext", local, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("guard panicked: %v", r)
						}
					}()
					if e := op(tc.s, tc.c); e == nil {
						t.Fatal("invalid scope accepted")
					}
				})
			}
		})
	}
}
