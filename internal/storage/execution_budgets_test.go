package storage

import (
	"context"
	"fmt"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
	"sync"
	"testing"
	"time"
)

func TestExecutionBudgetConcurrentChildrenAndSettlement(t *testing.T) {
	owner, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, _ := owner.ForTenant(tenant.Local)
	ctx := tenant.Into(context.Background(), tenant.Local)
	b := types.ExecutionBudget{ID: "pages", Project: "p", Timezone: "America/Denver", Unit: "pages", Limit: 5}
	if ok, err := store.ConfigureExecutionBudget(ctx, b, 0); err != nil || !ok {
		t.Fatal(ok, err)
	}
	now := time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC)
	if _, _, err := store.ReserveBudget(ctx, "p", "pages", "root", "", 1, now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	admitted := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint("child-", i)
			_, ok, err := store.ReserveBudget(ctx, "p", "pages", id, "root", 1, now)
			if err == nil && ok {
				admitted <- id
			}
		}(i)
	}
	wg.Wait()
	close(admitted)
	count := 0
	for id := range admitted {
		count++
		if err := store.SettleBudget(ctx, "p", "pages", id, "committed"); err != nil {
			t.Fatal(err)
		}
		if err := store.SettleBudget(ctx, "p", "pages", id, "cancelled"); err == nil {
			t.Fatal("committed work refunded")
		}
	}
	if count != 4 {
		t.Fatalf("admitted %d children", count)
	}
	if _, ok, err := store.ReserveBudget(ctx, "p", "pages", "root", "", 1, now.Add(24*time.Hour)); err != nil || ok {
		t.Fatal("retry charged twice", ok, err)
	}
	_, used, _, err := store.ExecutionBudget(ctx, "p", "pages", now)
	if err != nil || used != 5 {
		t.Fatal(used, err)
	}
	_, used, _, err = store.ExecutionBudget(ctx, "p", "pages", now.Add(24*time.Hour))
	if err != nil || used != 0 {
		t.Fatal(used, err)
	}
	b.Timezone = "UTC"
	if ok, err := store.ConfigureExecutionBudget(ctx, b, 1); err != nil || ok {
		t.Fatal("timezone reset accepted", ok, err)
	}
	if err := store.SettleBudget(ctx, "p", "pages", "missing", "cancelled"); err == nil {
		t.Fatal("missing reservation accepted")
	}
}

func TestSupervisorCheckpointHistoryAndConflict(t *testing.T) {
	owner, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, _ := owner.ForTenant(tenant.Local)
	ctx := tenant.Into(context.Background(), tenant.Local)
	value := types.SupervisorCheckpoint{ID: "checkpoint-1", Project: "p", Artifact: "sha1", Question: "approve?", Revision: 1, State: "pending"}
	if ok, err := store.CompareSupervisorCheckpoint(ctx, 0, &value); err != nil || !ok {
		t.Fatal(ok, err)
	}
	value.Revision = 2
	value.State = "answered"
	value.Answer = "yes"
	if ok, err := store.CompareSupervisorCheckpoint(ctx, 1, &value); err != nil || !ok {
		t.Fatal(ok, err)
	}
	value.Revision = 3
	value.Artifact = "sha2"
	value.Answer = ""
	value.State = "pending"
	if ok, err := store.CompareSupervisorCheckpoint(ctx, 1, &value); err != nil || ok {
		t.Fatal("stale update", ok, err)
	}
	if ok, err := store.CompareSupervisorCheckpoint(ctx, 2, &value); err != nil || !ok {
		t.Fatal(ok, err)
	}
	rows, err := store.SupervisorCheckpointVersions(ctx, "p", value.ID)
	if err != nil || len(rows) != 3 || rows[1].Answer != "yes" || rows[0].Answer != "" {
		t.Fatal(rows, err)
	}
	hidden, err := store.SupervisorCheckpoint(ctx, "other", value.ID)
	if err != nil || hidden != nil {
		t.Fatal(hidden, err)
	}
}
